<script lang="ts">
	import type { ActionData, PageData } from './$types';
	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head><title>Sign in · mcproxy Admin</title></svelte:head>

<main class="flex min-h-screen items-center justify-center bg-slate-950 px-6">
	<form method="POST" class="w-full max-w-sm rounded-2xl border border-slate-800 bg-slate-900 p-7 shadow-2xl shadow-black/30">
		<p class="text-sm font-semibold uppercase tracking-[0.25em] text-cyan-400">mcproxy</p>
		<h1 class="mt-3 text-2xl font-bold text-white">Admin sign in</h1>
		<p class="mt-2 text-sm text-slate-400">Authenticate to access security policy controls.</p>
		{#if !data.configured}<p class="mt-5 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">Set ADMIN_PANEL_PASSWORD and a 32+ character ADMIN_PANEL_SESSION_SECRET.</p>{/if}
		{#if form?.error}<p class="mt-5 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">{form.error}</p>{/if}
		<label class="mt-6 block text-sm text-slate-300">Username<input name="username" autocomplete="username" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		<label class="mt-4 block text-sm text-slate-300">Password<input name="password" type="password" autocomplete="current-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		<button class="mt-6 w-full rounded-lg bg-cyan-400 px-4 py-2.5 font-semibold text-slate-950 hover:bg-cyan-300 disabled:opacity-50" disabled={!data.configured}>Sign in</button>
	</form>
</main>
