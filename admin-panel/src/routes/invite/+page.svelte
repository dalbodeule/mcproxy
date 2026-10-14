<script lang="ts">
	import type { ActionData, PageData } from './$types';
	import { translate } from '#lib/i18n.js';
	let { data, form }: { data: PageData; form: ActionData } = $props();
	const t = (key: string) => translate(data.locale, key);
</script>

<svelte:head><title>{t('members.acceptTitle')} · mcproxy</title></svelte:head>
<main class="flex min-h-screen items-center justify-center bg-slate-950 px-6">
	<form method="POST" class="w-full max-w-sm rounded-2xl border border-slate-800 bg-slate-900 p-7 shadow-2xl shadow-black/30">
		<p class="text-sm font-semibold uppercase tracking-[0.25em] text-cyan-400">mcproxy</p>
		<h1 class="mt-3 text-2xl font-bold text-white">{t('members.acceptTitle')}</h1>
		<p class="mt-2 text-sm text-slate-400">{t('members.acceptHelp')}</p>
		{#if form?.error}<p role="alert" aria-live="assertive" class="mt-5 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">{form.error}</p>{/if}
		<input type="hidden" name="token" value={form?.token ?? data.token} />
		<label class="mt-6 block text-sm text-slate-300">{t('login.password')}<input name="password" type="password" minlength="12" autocomplete="new-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		<label class="mt-4 block text-sm text-slate-300">{t('members.confirmPassword')}<input name="confirmation" type="password" minlength="12" autocomplete="new-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		<button class="mt-6 w-full rounded-lg bg-cyan-400 px-4 py-2.5 font-semibold text-slate-950 hover:bg-cyan-300">{t('members.accept')}</button>
	</form>
</main>
