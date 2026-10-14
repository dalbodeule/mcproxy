import { defineEnvVars } from '@sveltejs/kit/env';

export const variables = defineEnvVars({
	MCPROXY_API_URL: {
		description: 'Internal mcproxy HTTP API URL',
		schema: (value) => value ?? ''
	},
	MCPROXY_ADMIN_TOKEN: {
		description: 'Server-only mcproxy admin token',
		schema: (value) => value ?? ''
	},
	MCPROXY_ADMIN_TOKEN_FILE: {
		description: 'Mounted file containing the server-only mcproxy admin token',
		schema: (value) => value ?? ''
	},
	MCPROXY_ADMIN_ACTOR_SECRET: {
		description: 'Shared server-only secret for signing admin audit actors',
		schema: (value) => value ?? ''
	},
	MCPROXY_ADMIN_ACTOR_SECRET_FILE: {
		description: 'Mounted file containing the shared admin audit actor secret',
		schema: (value) => value ?? ''
	},
	ADMIN_PANEL_USERNAME: {
		description: 'Admin panel login username',
		schema: (value) => value ?? 'admin'
	},
	ADMIN_PANEL_USERNAME_FILE: {
		description: 'Mounted file containing the admin panel login username',
		schema: (value) => value ?? ''
	},
	ADMIN_PANEL_PASSWORD: {
		description: 'Admin panel login password',
		schema: (value) => value ?? ''
	},
	ADMIN_PANEL_PASSWORD_FILE: {
		description: 'Mounted file containing the admin panel login password',
		schema: (value) => value ?? ''
	},
	ADMIN_PANEL_SESSION_SECRET: {
		description: 'Secret used to sign admin panel sessions',
		schema: (value) => value ?? ''
	},
	ADMIN_PANEL_SESSION_SECRET_FILE: {
		description: 'Mounted file containing the admin panel session signing secret',
	schema: (value) => value ?? ''
	},
	ADMIN_PANEL_DB_PATH: {
		description: 'Persistent SQLite path for admin users and invitations',
		schema: (value) => value ?? 'data/admin-panel.db'
	}
});
