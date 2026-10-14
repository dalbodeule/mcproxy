<script lang="ts">
	import type { ActionData, PageData } from './$types';
	import { translate } from '#lib/i18n.js';
	let { data, form }: { data: PageData; form: ActionData } = $props();
	const t = (key: string) => translate(data.locale, key);
</script>

<svelte:head><title>{t('account.title')} · mcproxy</title></svelte:head>
<main class="flex min-h-screen items-center justify-center bg-slate-950 px-6 py-10 text-slate-200">
	<div class="w-full max-w-lg">
		<header class="mb-6"><a href="/" class="text-sm text-cyan-300 hover:text-cyan-200">← {t('account.back')}</a><h1 class="mt-4 text-3xl font-bold text-white">{t('account.title')}</h1><p class="mt-2 text-slate-400">{t('account.help')} <span class="text-slate-200">{data.username}</span></p></header>
		{#if form?.error}<div role="alert" aria-live="assertive" class="mb-5 rounded-xl border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-300">{form.error}</div>{/if}
		{#if form?.success}<div role="status" aria-live="polite" class="mb-5 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 text-sm text-emerald-300">{form.success}</div>{/if}
		<form method="POST" action="?/changePassword" class="rounded-2xl border border-slate-800 bg-slate-900 p-7 shadow-2xl shadow-black/20">
			<label class="block text-sm text-slate-300">{t('account.current')}<input name="current_password" type="password" autocomplete="current-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
			<label class="mt-4 block text-sm text-slate-300">{t('account.new')}<input name="new_password" type="password" minlength="12" autocomplete="new-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
			<label class="mt-4 block text-sm text-slate-300">{t('account.confirm')}<input name="confirmation" type="password" minlength="12" autocomplete="new-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
			<button class="mt-6 w-full rounded-lg bg-cyan-400 px-4 py-2.5 font-semibold text-slate-950 hover:bg-cyan-300">{t('account.submit')}</button>
		</form>
		<section class="mt-6 rounded-2xl border border-slate-800 bg-slate-900 p-7">
			<h2 class="text-lg font-semibold text-white">{t('account.mfa')}</h2>
			{#if data.mfaEnabled}
				<p class="mt-2 text-sm text-emerald-300">{t('account.mfaEnabled')}</p>
				<p class="mt-4 text-sm text-slate-400">{t('account.mfaDisableHelp')}</p>
				<form method="POST" action="?/disableMFA" class="mt-4 space-y-3"><input name="password" type="password" autocomplete="current-password" aria-label={t('account.current')} placeholder={t('account.current')} required class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white" /><input name="code" inputmode="numeric" minlength="6" maxlength="6" aria-label={t('account.mfaCode')} placeholder={t('account.mfaCode')} required class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 font-mono text-white" /><button class="rounded-lg border border-red-500/50 px-4 py-2 text-sm text-red-300 hover:bg-red-500/10">{t('account.mfaDisable')}</button></form>
			{:else}
				<p class="mt-2 text-sm text-slate-400">{t('account.mfaHelp')}</p>
				<form method="POST" action="?/beginMFA"><button class="mt-4 rounded-lg border border-cyan-500/50 px-4 py-2 text-sm text-cyan-300 hover:bg-cyan-400/10">{t('account.mfaStart')}</button></form>
			{/if}
			{#if form?.mfaSetup}
				<div class="mt-5 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-100"><p>{t('account.mfaSecret')}: <code class="break-all">{form.mfaSetup.secret}</code></p><p class="mt-2 break-all">{t('account.mfaURI')}: <code>{form.mfaSetup.uri}</code></p><form method="POST" action="?/confirmMFA" class="mt-4 flex gap-2"><input name="code" inputmode="numeric" minlength="6" maxlength="6" aria-label={t('account.mfaCode')} placeholder={t('account.mfaCode')} required class="min-w-0 flex-1 rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-white" /><button class="rounded-lg bg-cyan-400 px-3 py-2 text-sm font-semibold text-slate-950">{t('account.mfaConfirm')}</button></form></div>
			{/if}
		</section>
	</div>
</main>
