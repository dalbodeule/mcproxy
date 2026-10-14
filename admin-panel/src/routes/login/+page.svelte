<script lang="ts">
	import type { ActionData, PageData } from './$types';
	import { translate } from '#lib/i18n.js';
	let { data, form }: { data: PageData; form: ActionData } = $props();
	const t = (key: string) => translate(data.locale, key);
	const mfaRequired = $derived(Boolean((form as ActionData & { mfaRequired?: boolean } | null)?.mfaRequired));
</script>

<svelte:head><title>{t('login.title')} · mcproxy Admin</title></svelte:head>

<main class="flex min-h-screen items-center justify-center bg-slate-950 px-6">
	<div class="w-full max-w-sm">
		<div class="mb-3 flex justify-end"><form method="POST" action="/locale"><label class="sr-only" for="login-locale">{t('common.language')}</label><select id="login-locale" name="locale" onchange={(event) => (event.currentTarget.form as HTMLFormElement).submit()} class="rounded border border-slate-700 bg-slate-950 px-2 py-1 text-xs text-slate-300"><option value="ko" selected={data.locale === 'ko'}>{t('lang.ko')}</option><option value="en" selected={data.locale === 'en'}>{t('lang.en')}</option><option value="ja" selected={data.locale === 'ja'}>{t('lang.ja')}</option><input type="hidden" name="returnTo" value="/login" /></select></form></div>
	<form method="POST" class="rounded-2xl border border-slate-800 bg-slate-900 p-7 shadow-2xl shadow-black/30">
		<input type="hidden" name="returnTo" value={data.returnTo} />
		<p class="text-sm font-semibold uppercase tracking-[0.25em] text-cyan-400">mcproxy</p>
		<h1 class="mt-3 text-2xl font-bold text-white">{t('login.title')}</h1>
		<p class="mt-2 text-sm text-slate-400">{t('login.help')}</p>
		{#if !data.configured}<p class="mt-5 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">{t('login.missing')}</p>{/if}
		{#if form?.error}<p role="alert" aria-live="assertive" class="mt-5 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">{form.error}</p>{/if}
		<label class="mt-6 block text-sm text-slate-300">{t('login.username')}<input name="username" autocomplete="username" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		<label class="mt-4 block text-sm text-slate-300">{t('login.password')}<input name="password" type="password" autocomplete="current-password" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 text-white outline-none focus:border-cyan-400" /></label>
		{#if mfaRequired}<label class="mt-4 block text-sm text-slate-300">{t('login.otp')}<input name="otp" inputmode="numeric" minlength="6" maxlength="6" autocomplete="one-time-code" required class="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2.5 font-mono text-white outline-none focus:border-cyan-400" /></label>{/if}
		<button class="mt-6 w-full rounded-lg bg-cyan-400 px-4 py-2.5 font-semibold text-slate-950 hover:bg-cyan-300 disabled:opacity-50" disabled={!data.configured}>{t('login.submit')}</button>
	</form>
	</div>
</main>
