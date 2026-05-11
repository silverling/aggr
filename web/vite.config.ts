import { resolve } from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig, loadEnv } from 'vite'
import { viteSingleFile } from 'vite-plugin-singlefile'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), '')
	const proxyTarget = env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'

	return {
		plugins: [vue(), tailwindcss(), viteSingleFile()],
		publicDir: false,
		server: {
			host: '127.0.0.1',
			port: Number(env.VITE_PORT || 5173),
			proxy: {
				'/api': {
					target: proxyTarget,
					changeOrigin: true,
				},
				'/v1': {
					target: proxyTarget,
					changeOrigin: true,
				},
				'/healthz': {
					target: proxyTarget,
					changeOrigin: true,
				},
			},
		},
		build: {
			outDir: resolve(__dirname, '../internal/webui/dist'),
			emptyOutDir: true,
		},
	}
})
