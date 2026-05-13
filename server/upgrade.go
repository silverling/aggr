package server

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// defaultGitHubReleaseRepository is the default owner/repository pair used
	// when downloading installer and self-upgrade assets.
	defaultGitHubReleaseRepository = "silverling/aggr"
	// releaseArchiveBinaryName is the executable filename stored inside release archives.
	releaseArchiveBinaryName = "aggr"
)

// runSelfUpgrade downloads the latest release archive that matches the current
// operating system and architecture, replaces the running executable on disk,
// and prints the follow-up restart guidance for service-managed installs.
func runSelfUpgrade(stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	executablePath, err := resolveExecutablePath()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	archiveURL := latestReleaseArchiveURL(githubReleaseRepository(), runtime.GOOS, runtime.GOARCH)
	_, _ = fmt.Fprintf(stdout, "Downloading latest release from %s\n", archiveURL)

	downloadedBinaryPath, err := downloadReleaseBinary(executablePath, archiveURL)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(downloadedBinaryPath)
	}()

	if err := replaceExecutableBinary(executablePath, downloadedBinaryPath); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Upgraded %s to the latest release.\n", executablePath)
	_, _ = fmt.Fprintln(stdout, "If aggr is managed by systemd, restart it with: sudo systemctl restart aggr")
	return nil
}

// githubReleaseRepository returns the owner/repository pair used for upgrade
// downloads, while allowing callers to override it for private forks.
func githubReleaseRepository() string {
	repository := strings.TrimSpace(os.Getenv("AGGR_GITHUB_REPO"))
	if repository == "" {
		return defaultGitHubReleaseRepository
	}

	return repository
}

// latestReleaseArchiveURL builds the stable GitHub latest-download URL for the
// release archive that matches the requested operating system and architecture.
func latestReleaseArchiveURL(repository, goos, goarch string) string {
	return fmt.Sprintf("https://github.com/%s/releases/latest/download/aggr-%s-%s.tar.gz", repository, goos, goarch)
}

// resolveExecutablePath returns the on-disk path that should be replaced during
// a self-upgrade, resolving symlinks when the platform exposes them.
func resolveExecutablePath() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	resolvedPath, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return executablePath, nil
		}
		return "", err
	}

	return resolvedPath, nil
}

// downloadReleaseBinary downloads the matching release archive, extracts the
// `aggr` executable into the current executable directory, and returns the
// temporary file path that should later replace the live binary.
func downloadReleaseBinary(currentExecutablePath, archiveURL string) (string, error) {
	directoryPath := filepath.Dir(currentExecutablePath)
	temporaryFile, err := os.CreateTemp(directoryPath, "aggr-upgrade-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}

	temporaryPath := temporaryFile.Name()
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			_ = temporaryFile.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	httpClient := &http.Client{
		Timeout: 2 * time.Minute,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}
	response, err := httpClient.Get(archiveURL)
	if err != nil {
		return "", fmt.Errorf("download release archive: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release archive: unexpected status %s", response.Status)
	}

	if err := extractReleaseBinary(temporaryFile, response.Body); err != nil {
		return "", err
	}

	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary file: %w", err)
	}

	cleanupNeeded = false
	return temporaryPath, nil
}

// extractReleaseBinary reads a release tarball stream, finds the `aggr`
// executable entry, and writes it to the provided destination file.
func extractReleaseBinary(destinationFile *os.File, archiveReader io.Reader) error {
	gzipReader, err := gzip.NewReader(archiveReader)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read tar archive: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != releaseArchiveBinaryName {
			continue
		}

		if _, err := io.Copy(destinationFile, tarReader); err != nil {
			return fmt.Errorf("write temporary executable: %w", err)
		}

		fileMode := os.FileMode(header.Mode)
		if fileMode == 0 {
			fileMode = 0o755
		}
		if err := destinationFile.Chmod(fileMode); err != nil {
			return fmt.Errorf("set executable permissions: %w", err)
		}

		return nil
	}

	return errors.New("release archive did not contain an aggr executable")
}

// replaceExecutableBinary atomically swaps the existing executable with the
// downloaded replacement while keeping a rollback path if the final rename fails.
func replaceExecutableBinary(currentExecutablePath, downloadedBinaryPath string) error {
	backupPath := currentExecutablePath + ".previous"
	_ = os.Remove(backupPath)

	if err := os.Rename(currentExecutablePath, backupPath); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}

	if err := os.Rename(downloadedBinaryPath, currentExecutablePath); err != nil {
		_ = os.Rename(backupPath, currentExecutablePath)
		return fmt.Errorf("install upgraded executable: %w", err)
	}

	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove executable backup: %w", err)
	}

	return nil
}
