<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import type { ActionData, PageData } from './$types';
	import { formatDateTime, formatDuration, formatNumber, translate } from '#lib/i18n.js';

	let { data, form }: { data: PageData; form: ActionData } = $props();
	let refreshing = $state(false);
	let hasAttention = $derived(data.metrics.gate_ready !== 1 || (data.metrics.distributed_enabled === 1 && data.metrics.distributed_healthy !== 1) || data.metrics.snapshot_cache_age_seconds > 30 || data.metrics.audit_write_errors_total > 0);
	let canManageServers = $derived(data.role === 'owner' || data.role === 'admin');
	let canToggleServers = $derived(canManageServers || data.role === 'operator');
	const t = (key: string) => translate(data.locale, key);
	const auditEventLabels: Record<string, string> = {
		'connect.blocked': 'audit.blocked',
		'policy.update': 'audit.policy',
		'geo.update': 'audit.geo',
		'policy.reset': 'audit.reset',
		'server.create': 'audit.server',
		'server.update': 'audit.serverUpdate',
		'server.delete': 'audit.serverDelete',
		'server.bungeeguard_token.rotate': 'audit.tokenRotate',
		'rule.create': 'audit.rule',
		'rule.delete': 'audit.ruleDelete'
	};
	function auditEventLabel(event: string) {
		const key = auditEventLabels[event];
		return key ? t(key) : event;
	}
	function serverHealthLabel(status: string) {
		return status === 'healthy' ? t('servers.healthy') : status === 'unreachable' ? t('servers.unreachable') : status === 'disabled' ? t('servers.disabledHealth') : t('servers.unknown');
	}
	function serverHealthClass(status: string) {
		return status === 'healthy' ? 'text-emerald-300' : status === 'unreachable' ? 'text-red-300' : 'text-slate-500';
	}
	function serverGateLabel(health: { gate_registered?: boolean; gate_token_applied?: boolean } | undefined) {
		if (!health || health.gate_registered === undefined) return '';
		if (!health.gate_registered) return t('servers.gatePending');
		if (health.gate_token_applied === false) return t('servers.tokenPending');
		return t('servers.gateSynced');
	}
	function auditValue(value: unknown) {
		if (typeof value === 'string') return value;
		try { return JSON.stringify(value); } catch { return String(value); }
	}

	async function refresh() {
		refreshing = true;
		await invalidateAll();
		refreshing = false;
	}

</script>

<svelte:head>
	<title>mcproxy · {t('brand.title')}</title>
	<meta name="description" content="mcproxy security proxy operations panel" />
</svelte:head>


<a href="#main-content" class="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-cyan-400 focus:px-4 focus:py-2 focus:font-semibold focus:text-slate-950">{t('common.skipContent')}</a>
<main id="main-content" class="min-h-screen bg-slate-950">
	<div class="mx-auto max-w-7xl px-6 pt-4 text-right lg:px-10"><a href="/account" class="text-sm text-slate-400 hover:text-cyan-300">{t('account.title')}</a></div>
	<div class="mx-auto max-w-7xl px-6 py-8 lg:px-10">
		<header class="mb-8 flex flex-wrap items-end justify-between gap-4">
			<div>
				<p class="mb-2 text-sm font-semibold uppercase tracking-[0.25em] text-cyan-400">{t('brand.control')}</p>
				<h1 class="text-3xl font-bold tracking-tight text-white">{t('brand.title')}</h1>
				<p class="mt-2 text-slate-400">{t('brand.subtitle')}</p>
			</div>
				<div class="flex flex-wrap items-center justify-end gap-2"><form method="POST" action="/locale" class="flex items-center gap-2"><label class="sr-only" for="locale">{t('common.language')}</label><select id="locale" name="locale" onchange={(event) => (event.currentTarget.form as HTMLFormElement).submit()} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-slate-200" aria-label={t('common.language')}><option value="ko" selected={data.locale === 'ko'}>{t('lang.ko')}</option><option value="en" selected={data.locale === 'en'}>{t('lang.en')}</option><option value="ja" selected={data.locale === 'ja'}>{t('lang.ja')}</option></select><input type="hidden" name="returnTo" value={data.returnTo} /></form>{#if data.role === 'owner' || data.role === 'admin'}<a href="/members" class="rounded-lg border border-slate-700 px-3 py-2 text-sm text-slate-200 hover:border-cyan-400">{t('members.manage')}</a>{/if}{#if data.username}<span class="rounded-lg border border-slate-800 px-3 py-2 text-xs text-slate-400">{t('session.signedInAs')} <span class="text-slate-200">{data.username}</span></span>{/if}<button class="rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-200 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-50" onclick={refresh} disabled={refreshing}>{refreshing ? t('action.refreshing') : t('action.refresh')}</button><form method="POST" action="/logout"><button class="rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-400 hover:border-red-400 hover:text-red-300">{t('action.signout')}</button></form></div>
		</header>

		{#if !data.apiAvailable}
			<div role="status" aria-live="polite" class="mb-6 rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 text-amber-200">{t('status.apiUnavailable')}</div>
		{/if}

		<section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-6">
			{#each [
				[t('card.uptime'), data.stats ? formatDuration(data.locale, data.stats.uptime_seconds) : '—'],
				[t('card.servers'), data.stats ? formatNumber(data.locale, data.stats.servers) : '—'],
				[t('card.policies'), data.stats ? formatNumber(data.locale, data.stats.policies) : '—'],
				[t('card.rules'), data.stats ? formatNumber(data.locale, data.stats.rules) : '—'],
				[t('card.blocked'), data.stats ? formatNumber(data.locale, data.stats.blocks) : '—'],
				[t('card.blockRate'), data.stats?.attempts ? `${formatNumber(data.locale, ((data.stats.decision_blocks ?? (data.stats.attempts - (data.stats.allowed ?? 0))) / data.stats.attempts) * 100)}%` : '—']
			] as card}
				<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-5 shadow-lg shadow-black/10">
					<p class="text-sm text-slate-400">{card[0]}</p>
					<p class="mt-2 text-2xl font-semibold text-white">{card[1]}</p>
				</div>
			{/each}
		</section>

		<section class="mt-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-8" aria-label={t('status.runtime')}>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.build')}</p><p class="mt-2 truncate font-mono text-sm font-semibold text-white" title={data.metrics.build_version || 'dev'}>{data.metrics.build_version || 'dev'}</p><p class="mt-1 truncate text-xs text-slate-500" title={data.metrics.build_commit}>{data.metrics.build_commit || 'unknown'}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><div class="flex items-center justify-between"><p class="text-sm text-slate-400">{t('status.gate')}</p><span class={data.metrics.gate_ready === 1 ? 'rounded-full bg-emerald-400/10 px-2 py-1 text-xs text-emerald-300' : 'rounded-full bg-amber-400/10 px-2 py-1 text-xs text-amber-300'}>{data.metrics.gate_ready === 1 ? t('status.ready') : t('status.degraded')}</span></div><p class="mt-2 text-xl font-semibold text-white">{data.metrics.gate_ready === 1 ? t('status.ready') : t('status.degraded')}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.players')}</p><p class="mt-2 text-xl font-semibold text-white">{formatNumber(data.locale, data.metrics.gate_players)}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.backends')}</p><p class="mt-2 text-xl font-semibold text-white">{formatNumber(data.locale, data.metrics.gate_backends)}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.cacheAge')}</p><p class="mt-2 text-xl font-semibold text-white">{formatNumber(data.locale, Math.round(data.metrics.snapshot_cache_age_seconds))}s</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><div class="flex items-center justify-between"><p class="text-sm text-slate-400">{t('card.redis')}</p><span class={data.metrics.distributed_enabled === 1 && data.metrics.distributed_healthy !== 1 ? 'rounded-full bg-amber-400/10 px-2 py-1 text-xs text-amber-300' : 'rounded-full bg-emerald-400/10 px-2 py-1 text-xs text-emerald-300'}>{data.metrics.distributed_enabled !== 1 ? t('status.notConfigured') : data.metrics.distributed_healthy === 1 ? t('status.ready') : t('status.degraded')}</span></div><p class="mt-2 text-xl font-semibold text-white">{data.metrics.distributed_enabled !== 1 ? '—' : data.metrics.distributed_healthy === 1 ? t('status.ready') : t('status.degraded')}</p>{#if data.metrics.distributed_enabled === 1}<p class="mt-1 text-xs text-slate-500">{t('status.fallbacks')}: {formatNumber(data.locale, data.metrics.distributed_fallbacks_total)}{data.metrics.distributed_fail_closed === 1 ? ` · ${t('status.failClosed')}` : ''}</p>{/if}</div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.evalLatency')}</p><p class="mt-2 text-xl font-semibold text-white">{data.metrics.evaluation_latency_seconds_count > 0 ? formatNumber(data.locale, (data.metrics.evaluation_latency_seconds_total / data.metrics.evaluation_latency_seconds_count) * 1000) : '—'}ms</p><p class="mt-1 text-xs text-slate-500">{formatNumber(data.locale, data.metrics.evaluation_latency_seconds_count)} {t('status.evaluations')}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.dbPool')}</p><p class="mt-2 text-xl font-semibold text-white">{formatNumber(data.locale, data.metrics.db_in_use_connections)} / {formatNumber(data.locale, data.metrics.db_open_connections)}</p><p class="mt-1 text-xs text-slate-500">{t('status.dbWaits')}: {formatNumber(data.locale, data.metrics.db_wait_count_total)}</p></div>
			<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-4"><p class="text-sm text-slate-400">{t('card.dbLatency')}</p><p class="mt-2 text-xl font-semibold text-white">{data.metrics.store_stats_latency_seconds_count > 0 ? formatNumber(data.locale, (data.metrics.store_stats_latency_seconds_total / data.metrics.store_stats_latency_seconds_count) * 1000) : '—'}ms</p><p class="mt-1 text-xs text-slate-500">{formatNumber(data.locale, data.metrics.store_stats_latency_seconds_count)} {t('status.dbQueries')}</p></div>
		</section>

		<section class="mt-4 rounded-xl border border-slate-800 bg-slate-900/80 p-5" aria-live="polite" aria-label={t('status.attentionTitle')}>
			<div class="mb-3 flex items-center justify-between gap-3"><h2 class="font-semibold text-white">{t('status.attentionTitle')}</h2><span class={hasAttention ? 'rounded-full bg-amber-400/10 px-2 py-1 text-xs text-amber-300' : 'rounded-full bg-emerald-400/10 px-2 py-1 text-xs text-emerald-300'}>{hasAttention ? t('status.degraded') : t('status.ready')}</span></div>
			{#if !hasAttention}<p class="text-sm text-emerald-200">{t('status.noAlerts')}</p>{:else}<ul class="space-y-2 text-sm">{#if data.metrics.gate_ready !== 1}<li class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-red-200"><span><strong class="mr-2 rounded bg-red-400/20 px-1.5 py-0.5 text-[10px] uppercase tracking-wide">{t('status.critical')}</strong>{t('status.gateUnavailable')}</span><a href="/?view=servers" class="rounded border border-red-400/40 px-2 py-1 text-xs hover:bg-red-400/10">{t('nav.servers')}</a></li>{/if}{#if data.metrics.distributed_enabled === 1 && data.metrics.distributed_healthy !== 1}<li class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-amber-200"><span><strong class="mr-2 rounded bg-amber-400/20 px-1.5 py-0.5 text-[10px] uppercase tracking-wide">{t('status.warning')}</strong>{t('status.redisDegraded')}</span><a href="/?view=dashboard" class="rounded border border-amber-400/40 px-2 py-1 text-xs hover:bg-amber-400/10">{t('nav.dashboard')}</a></li>{/if}{#if data.metrics.snapshot_cache_age_seconds > 30}<li class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-amber-200"><span><strong class="mr-2 rounded bg-amber-400/20 px-1.5 py-0.5 text-[10px] uppercase tracking-wide">{t('status.warning')}</strong>{t('status.cacheStale')}</span><a href="/?view=policies" class="rounded border border-amber-400/40 px-2 py-1 text-xs hover:bg-amber-400/10">{t('nav.policies')}</a></li>{/if}{#if data.metrics.audit_write_errors_total > 0}<li class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-amber-200"><span><strong class="mr-2 rounded bg-amber-400/20 px-1.5 py-0.5 text-[10px] uppercase tracking-wide">{t('status.warning')}</strong>{t('status.auditWritesFailed')} ({formatNumber(data.locale, data.metrics.audit_write_errors_total)})</span><a href="/?view=audit" class="rounded border border-amber-400/40 px-2 py-1 text-xs hover:bg-amber-400/10">{t('nav.audit')}</a></li>{/if}</ul>{/if}
		</section>

		<nav aria-label={t('nav.title')} class="mt-8 overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/80 p-2">
			<div class="flex min-w-max gap-2">
				{#each [['dashboard', 'nav.dashboard'], ['policies', 'nav.policies'], ['servers', 'nav.servers'], ['audit', 'nav.audit']] as item}
					<a href={`/?view=${item[0]}`} aria-current={data.view === item[0] ? 'page' : undefined} class={data.view === item[0] ? 'rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950' : 'rounded-lg px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white'}>{t(item[1])}</a>
				{/each}
			</div>
		</nav>

		{#if data.view === 'policies'}
		{#if data.canEditGlobal}<section class="mt-8 grid gap-6 xl:grid-cols-2">
			<form method="POST" action="?/updatePolicy" class="rounded-xl border border-slate-800 bg-slate-900/80 p-5">
				<div class="mb-4"><h2 class="font-semibold text-white">{t('policy.global')}</h2><p class="mt-1 text-sm text-slate-400">{t('policy.globalHelp')}</p></div>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="text-xs text-slate-400">{t('policy.ip10')}<input name="ip_burst_10s" type="number" min="0" value={data.policy?.ip_burst_10s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">{t('policy.ip60')}<input name="ip_burst_60s" type="number" min="0" value={data.policy?.ip_burst_60s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">{t('policy.name10')}<input name="name_burst_10s" type="number" min="0" value={data.policy?.name_burst_10s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">{t('policy.name60')}<input name="name_burst_60s" type="number" min="0" value={data.policy?.name_burst_60s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
				</div>
				<label class="mt-3 flex items-center gap-2 text-sm text-slate-300"><input name="deny_first" type="checkbox" checked={data.policy?.deny_first ?? false} /> {t('policy.denyFirst')}</label>
				<label class="mt-3 block text-xs text-slate-400">{t('policy.kickMessage')}<textarea name="kick_message" rows="2" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white">{data.policy?.kick_message ?? ''}</textarea></label>
				<div class="mt-4 flex flex-wrap gap-2"><button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">{t('action.savePolicy')}</button><button formaction="?/resetPolicy" formmethod="POST" class="rounded-lg border border-amber-500/50 px-4 py-2 text-sm text-amber-300 hover:bg-amber-400/10" onclick={(event) => { if (!confirm(t('common.resetConfirm'))) event.preventDefault(); }}>{t('action.resetDefaults')}</button></div>
			</form>

			<form method="POST" action="?/updateGeo" class="rounded-xl border border-slate-800 bg-slate-900/80 p-5">
				<div class="mb-4"><h2 class="font-semibold text-white">{t('geo.title')}</h2><p class="mt-1 text-sm text-slate-400">{t('geo.help')}</p></div>
				<label class="block text-xs text-slate-400">{t('geo.mode')}<select name="mode" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"><option value="disabled" selected={data.geo?.mode === 'disabled'}>{t('geo.disabled')}</option><option value="allow" selected={data.geo?.mode === 'allow'}>{t('geo.allow')}</option><option value="deny" selected={data.geo?.mode === 'deny'}>{t('geo.deny')}</option></select></label>
				<label class="mt-3 block text-xs text-slate-400">{t('geo.countries')}<input name="countries" value={data.geo?.countries?.join(', ') ?? ''} placeholder="KR, JP, US" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white placeholder:text-slate-600" /></label>
				<div class="mt-4 flex flex-wrap gap-2"><button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">{t('geo.save')}</button><button formaction="?/resetGeo" formmethod="POST" class="rounded-lg border border-amber-500/50 px-4 py-2 text-sm text-amber-300 hover:bg-amber-400/10" onclick={(event) => { if (!confirm(t('common.resetConfirm'))) event.preventDefault(); }}>{t('geo.disable')}</button></div>
			</form>
		</section>{/if}

		{#if data.canEditGlobal}<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="border-b border-slate-800 px-5 py-4"><h2 class="font-semibold text-white">{t('rules.title')}</h2><p class="mt-1 text-sm text-slate-400">{t('rules.help')}</p></div>
			<form method="POST" action="?/createRule" class="grid gap-2 border-b border-slate-800 bg-slate-950/40 p-5 md:grid-cols-4">
				<select name="kind" aria-label={t('rules.title')} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white"><option value="ip-allow">{t('rules.ipAllow')}</option><option value="ip-deny">{t('rules.ipDeny')}</option><option value="name-allow">{t('rules.nameAllow')}</option><option value="name-deny">{t('rules.nameDeny')}</option><option value="asn-allow">{t('rules.asnAllow')}</option><option value="asn-deny">{t('rules.asnDeny')}</option></select>
				<input name="target" required aria-label={t('common.target')} placeholder={t('common.target')} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white placeholder:text-slate-600" />
				<input name="reason" aria-label={t('common.reason')} placeholder={t('common.reason')} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white placeholder:text-slate-600" />
				<button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">{t('rules.add')}</button>
			</form>
			<div class="divide-y divide-slate-800">
				{#each data.rules as rule}
					<div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3 text-sm"><div><span class="rounded bg-slate-800 px-2 py-1 text-xs text-cyan-300">{rule.kind}</span><span class="ml-3 font-mono text-slate-200">{rule.target}</span>{#if rule.reason}<span class="ml-3 text-slate-500">{rule.reason}</span>{/if}</div><form method="POST" action="?/removeRule" onsubmit={(event) => { if (!confirm(t('common.deleteConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={rule.id} /><button class="rounded border border-red-900 px-2 py-1 text-xs text-red-300 hover:border-red-400">{t('action.delete')}</button></form></div>
				{:else}<div class="px-5 py-8 text-center text-sm text-slate-500">{t('rules.empty')}</div>{/each}
			</div>
			<div class="flex items-center justify-between border-t border-slate-800 px-5 py-3 text-xs text-slate-500"><span>{data.rules.length ? `${data.ruleOffset + 1}-${data.ruleOffset + data.rules.length}` : '0'}</span><div class="flex gap-2">{#if data.ruleOffset > 0}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=policies&rule_offset=${Math.max(0, data.ruleOffset - data.ruleLimit)}`}>{t('rules.previous')}</a>{/if}{#if data.ruleHasNext}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=policies&rule_offset=${data.ruleOffset + data.ruleLimit}`}>{t('rules.next')}</a>{/if}</div></div>
		</section>{/if}

		<section class="mt-8 rounded-xl border border-slate-800 bg-slate-900/80 p-5">
			<div class="mb-4"><h2 class="font-semibold text-white">{t('policy.serverTitle')}</h2><p class="mt-1 text-sm text-slate-400">{t('policy.serverHelp')}</p></div>
			<div class="grid gap-4 lg:grid-cols-2">
				{#each data.servers as server}
					{@const policy = data.serverPolicies[server.id]}
						{#if canManageServers}						<form method="POST" action="?/updateServerPolicy" class="rounded-lg border border-slate-800 bg-slate-950/50 p-4">
						<div class="mb-3 flex items-center justify-between"><h3 class="font-medium text-white">{server.name}</h3><input type="hidden" name="server_id" value={server.id} /><span class="text-xs text-slate-500">{policy ? t('policy.override') : t('policy.inherited')}</span></div>
						<div class="grid gap-2 sm:grid-cols-2">
							<input name="ip_burst_10s" type="number" min="0" value={policy?.ip_burst_10s ?? data.policy?.ip_burst_10s ?? 0} aria-label={`${server.name} ${t('policy.ip10')}`} placeholder="IP / 10s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="ip_burst_60s" type="number" min="0" value={policy?.ip_burst_60s ?? data.policy?.ip_burst_60s ?? 0} aria-label={`${server.name} ${t('policy.ip60')}`} placeholder="IP / 60s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="name_burst_10s" type="number" min="0" value={policy?.name_burst_10s ?? data.policy?.name_burst_10s ?? 0} aria-label={`${server.name} ${t('policy.name10')}`} placeholder="Name / 10s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="name_burst_60s" type="number" min="0" value={policy?.name_burst_60s ?? data.policy?.name_burst_60s ?? 0} aria-label={`${server.name} ${t('policy.name60')}`} placeholder="Name / 60s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
						</div>
						<label class="mt-3 flex items-center gap-2 text-sm text-slate-300"><input name="deny_first" type="checkbox" checked={policy?.deny_first ?? data.policy?.deny_first ?? false} /> {t('policy.denyFirst')}</label>
						<div class="mt-3 flex flex-wrap gap-2"><button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">{t('policy.saveServer')}</button><button formaction="?/resetServerPolicy" formmethod="POST" class="rounded border border-amber-500/50 px-3 py-1.5 text-xs text-amber-300 hover:bg-amber-400/10" onclick={(event) => { if (!confirm(t('common.resetConfirm'))) event.preventDefault(); }}>{t('policy.inheritGlobal')}</button></div>
				</form>{/if}
				{/each}
			</div>
		</section>

		<section class="mt-8 rounded-xl border border-slate-800 bg-slate-900/80 p-5">
			<div class="mb-4"><h2 class="font-semibold text-white">{t('scope.title')}</h2><p class="mt-1 text-sm text-slate-400">{t('scope.help')}</p></div>
			<div class="grid gap-4 lg:grid-cols-2">
				{#each data.servers as server}
					{@const serverGeo = data.serverGeos[server.id]}
					{@const scopedRules = data.serverRules[server.id] ?? []}
					<div class="rounded-lg border border-slate-800 bg-slate-950/50 p-4">
						<h3 class="mb-3 font-medium text-white">{server.name}</h3>
						{#if canManageServers}<form method="POST" action="?/updateGeo" class="space-y-2">
							<input type="hidden" name="server_id" value={server.id} />
							<label class="block text-xs text-slate-400">{t('common.geoMode')}<select name="mode" class="mt-1 w-full rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white"><option value="disabled" selected={serverGeo?.mode === 'disabled' || !serverGeo}>{t('geo.disabled')}</option><option value="allow" selected={serverGeo?.mode === 'allow'}>{t('geo.allow')}</option><option value="deny" selected={serverGeo?.mode === 'deny'}>{t('geo.deny')}</option></select></label>
							<label class="block text-xs text-slate-400">{t('common.countries')}<input name="countries" value={serverGeo?.countries?.join(', ') ?? ''} placeholder="KR, JP, US" class="mt-1 w-full rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white placeholder:text-slate-600" /></label>
							<div class="flex flex-wrap gap-2"><button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">{t('scope.saveGeo')}</button><button formaction="?/resetGeo" formmethod="POST" class="rounded border border-amber-500/50 px-3 py-1.5 text-xs text-amber-300 hover:bg-amber-400/10" onclick={(event) => { if (!confirm(t('common.resetConfirm'))) event.preventDefault(); }}>{t('scope.resetGeo')}</button></div>
						</form>
						<form method="POST" action="?/createRule" class="mt-4 grid gap-2 sm:grid-cols-3">
							<input type="hidden" name="server_id" value={server.id} />
			<select name="kind" aria-label={`${server.name} ${t('rules.title')}`} class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-xs text-white"><option value="ip-allow">{t('rules.ipAllow')}</option><option value="ip-deny">{t('rules.ipDeny')}</option><option value="name-allow">{t('rules.nameAllow')}</option><option value="name-deny">{t('rules.nameDeny')}</option><option value="asn-allow">{t('rules.asnAllow')}</option><option value="asn-deny">{t('rules.asnDeny')}</option></select>
			<input name="target" required aria-label={`${server.name} ${t('common.ipCidrName')}`} placeholder={t('common.ipCidrName')} class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-xs text-white placeholder:text-slate-600" />
							<button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">{t('scope.addRule')}</button>
						</form>
						<div class="mt-3 space-y-1">
							{#each scopedRules as rule}
								<div class="flex items-center justify-between gap-2 text-xs"><span><span class="text-cyan-300">{rule.kind}</span> <span class="font-mono text-slate-300">{rule.target}</span></span><form method="POST" action="?/removeRule" onsubmit={(event) => { if (!confirm(t('common.deleteConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={rule.id} /><input type="hidden" name="server_id" value={server.id} /><button class="text-red-300 hover:text-red-200">{t('action.delete')}</button></form></div>
							{:else}<p class="text-xs text-slate-600">{t('scope.noRules')}</p>{/each}
							</div>
						{/if}</div>
				{:else}<p class="text-sm text-slate-500">{t('scope.noServer')}</p>{/each}
			</div>
		</section>
		{/if}

		{#if data.view === 'servers'}
		<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="flex flex-wrap items-center justify-between gap-4 border-b border-slate-800 px-5 py-4">
				<div>
					<h2 class="font-semibold text-white">{t('servers.title')}</h2>
					<p class="mt-1 text-sm text-slate-400">{t('servers.help')}</p><p class="mt-1 text-xs text-cyan-300/80">{t('servers.distributionHelp')}</p>
				</div>
				<span class="rounded-full bg-cyan-400/10 px-3 py-1 text-xs font-medium text-cyan-300">{formatNumber(data.locale, data.servers.length)} / {formatNumber(data.locale, data.serverTotal)} {t('servers.configured')}</span>
			</div>
			<form method="GET" class="grid gap-2 border-b border-slate-800 bg-slate-950/30 p-4 sm:grid-cols-[minmax(0,1fr)_auto_auto]"><input type="hidden" name="view" value="servers" /><label class="sr-only" for="server-search">{t('servers.search')}</label><input id="server-search" name="server_q" value={data.serverSearch} placeholder={t('servers.search')} class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white placeholder:text-slate-600" /><label class="sr-only" for="server-enabled-filter">{t('servers.statusFilter')}</label><select id="server-enabled-filter" name="server_enabled" class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"><option value="" selected={!data.serverEnabledFilter}>{t('servers.allStatuses')}</option><option value="enabled" selected={data.serverEnabledFilter === 'enabled'}>{t('servers.onlyEnabled')}</option><option value="disabled" selected={data.serverEnabledFilter === 'disabled'}>{t('servers.onlyDisabled')}</option></select><button class="rounded-lg border border-cyan-500/50 px-4 py-2 text-sm text-cyan-300 hover:bg-cyan-400/10">{t('action.refresh')}</button></form>
			{#if data.distributionGroups.length}<div class="border-b border-cyan-500/20 bg-cyan-500/5 px-5 py-4"><h3 class="text-sm font-semibold text-cyan-100">{t('servers.distributionGroups')}</h3><div class="mt-3 grid gap-2 lg:grid-cols-2">{#each data.distributionGroups as group}<div class="rounded-lg border border-slate-800 bg-slate-950/40 p-3 text-xs"><p class="font-mono text-cyan-300">{group.route}</p><div class="mt-2 grid gap-1">{#each group.backends as backend}<div class="flex flex-wrap items-center justify-between gap-2"><span class="text-slate-300">{backend.name}</span><span class={serverHealthClass(data.serverHealth[backend.id]?.status ?? 'unknown')}>{serverHealthLabel(data.serverHealth[backend.id]?.status ?? 'unknown')}{#if data.serverHealth[backend.id]?.gate_registered !== undefined} · {serverGateLabel(data.serverHealth[backend.id])}{/if}</span></div>{/each}</div><p class="mt-2 text-slate-500">{group.backends.length} {t('servers.backendsInGroup')}</p></div>{/each}</div></div>{/if}
			{#if data.canCreateServers}<form method="POST" action="?/create" class="grid gap-2 border-b border-slate-800 bg-slate-950/40 p-5 md:grid-cols-6">
				<input name="name" required aria-label={t('common.name')} placeholder={t('common.name')} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="upstream" required aria-label={t('common.upstream')} placeholder="127.0.0.1:25566" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="public_host" aria-label={t('common.publicHostname')} placeholder={`${t('common.publicHostname')} (${t('common.optional')})`} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="public_addr" aria-label={t('common.publicAddress')} placeholder={`${t('common.publicAddress')} (${t('common.optional')})`} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<select name="forwarding_mode" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white" aria-label={t('servers.forwarding')}><option value="">{t('common.globalFallback')}</option><option value="none">{t('common.none')}</option><option value="legacy">{t('common.legacy')}</option><option value="bungeeguard">{t('common.bungee')}</option><option value="velocity">{t('common.velocity')}</option></select>
				<input name="ping_motd" maxlength="256" aria-label={t('common.pingMotd')} placeholder={`${t('common.pingMotd')} (${t('common.optional')})`} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="priority" type="number" min="0" aria-label={t('common.priority')} value="0" placeholder={t('common.priority')} class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 transition hover:bg-cyan-300">{t('servers.add')}</button>
			</form>{/if}
			{#if form?.error}<div role="alert" aria-live="assertive" class="border-b border-red-500/30 bg-red-500/10 px-5 py-3 text-sm text-red-300">{form.error}</div>{/if}
			{#if form?.success}<div role="status" aria-live="polite" class="border-b border-emerald-500/30 bg-emerald-500/10 px-5 py-3 text-sm text-emerald-300">
				<p>{form.success}</p>
				{#if form.tokenRotation}<div class="mt-3 rounded-lg border border-emerald-400/20 bg-slate-950/40 p-3"><p class="text-xs text-slate-300">{t('servers.rotationApply')}</p><dl class="mt-2 grid gap-2 text-xs sm:grid-cols-2"><div><dt class="text-slate-500">{t('servers.rotationPrevious')}</dt><dd class="mt-1 break-all font-mono text-amber-200">{form.tokenRotation.previous || '—'}</dd></div><div><dt class="text-slate-500">{t('servers.rotationCurrent')}</dt><dd class="mt-1 break-all font-mono text-emerald-200">{form.tokenRotation.current || '—'}</dd></div></dl></div>{/if}
			</div>{/if}
			<div class="grid gap-3 p-4 md:hidden">
				{#each data.servers as server}
					<article class="rounded-xl border border-slate-800 bg-slate-950/60 p-4">
						<div class="flex items-start justify-between gap-3"><div><h3 class="font-semibold text-white">{server.name}</h3><p class="mt-1 break-all font-mono text-xs text-slate-400">{server.upstream}</p></div><div class="flex flex-wrap justify-end gap-2"><span class={server.enabled ? 'rounded-full bg-emerald-400/10 px-2 py-1 text-xs text-emerald-300' : 'rounded-full bg-slate-700 px-2 py-1 text-xs text-slate-400'}>{server.enabled ? t('servers.enabled') : t('servers.disabled')}</span><span class={`rounded-full bg-slate-800 px-2 py-1 text-xs ${serverHealthClass(data.serverHealth[server.id]?.status ?? 'unknown')}`}>{serverHealthLabel(data.serverHealth[server.id]?.status ?? 'unknown')}</span>{#if data.serverHealth[server.id]?.gate_registered !== undefined}<span class="rounded-full bg-slate-800 px-2 py-1 text-xs text-cyan-300">{serverGateLabel(data.serverHealth[server.id])}</span>{/if}</div></div>
						<dl class="mt-4 grid grid-cols-2 gap-3 text-xs"><div><dt class="text-slate-500">{t('servers.forwarding')}</dt><dd class="mt-1 text-cyan-300">{server.forwarding_mode || t('common.globalFallback')}</dd></div><div><dt class="text-slate-500">{t('common.priority')}</dt><dd class="mt-1 text-slate-300">{server.priority}</dd></div><div class="col-span-2"><dt class="text-slate-500">{t('common.publicRoute')}</dt><dd class="mt-1 break-all text-slate-300">{server.public_host || server.public_addr || t('servers.defaultRoute')}</dd></div></dl>
						<div class="mt-4 flex flex-wrap gap-2">{#if canToggleServers}<form method="POST" action="?/setEnabled"><input type="hidden" name="id" value={server.id} /><input type="hidden" name="enabled" value={String(!server.enabled)} /><button class="rounded border border-slate-700 px-3 py-1.5 text-xs text-slate-200 hover:border-cyan-400">{server.enabled ? t('servers.disable') : t('servers.enable')}</button></form>{/if}{#if data.canManageBungeeGuardTokens}<form method="POST" action="?/rotateBungeeGuardToken" onsubmit={(event) => { if (!confirm(t('common.rotateConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="rounded border border-amber-500/50 px-3 py-1.5 text-xs text-amber-200 hover:border-amber-300">{t('servers.rotate')}</button></form>{/if}{#if canManageServers}<form method="POST" action="?/remove" onsubmit={(event) => { if (!confirm(t('common.deleteConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="rounded border border-red-900 px-3 py-1.5 text-xs text-red-300 hover:border-red-400">{t('action.delete')}</button></form>{/if}</div>
					</article>
				{:else}<p class="px-1 py-6 text-center text-sm text-slate-500">{t('servers.noServers')}</p>{/each}
			</div>
			<div class="flex items-center justify-between border-t border-slate-800 px-5 py-3 text-xs text-slate-500 md:hidden"><span>{data.serverTotal ? `${data.serverOffset + 1}-${Math.min(data.serverOffset + data.serverLimit, data.serverTotal)} / ${data.serverTotal}` : '0'}</span><div class="flex gap-2">{#if data.serverHasPrevious}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=servers&server_q=${encodeURIComponent(data.serverSearch)}&server_enabled=${encodeURIComponent(data.serverEnabledFilter)}&server_offset=${Math.max(0, data.serverOffset - data.serverLimit)}`}>{t('servers.previous')}</a>{/if}{#if data.serverHasNext}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=servers&server_q=${encodeURIComponent(data.serverSearch)}&server_enabled=${encodeURIComponent(data.serverEnabledFilter)}&server_offset=${data.serverOffset + data.serverLimit}`}>{t('servers.next')}</a>{/if}</div></div>
			<div class="hidden overflow-x-auto md:block">
				<table class="w-full min-w-[980px] text-left text-sm">
					<thead class="bg-slate-950/70 text-xs uppercase tracking-wider text-slate-500">
						<tr><th class="px-5 py-3">{t('common.name')}</th><th class="px-5 py-3">{t('common.publicRoute')}</th><th class="px-5 py-3">{t('common.upstream')}</th><th class="px-5 py-3">{t('servers.forwarding')}</th><th class="px-5 py-3">{t('common.priority')}</th><th class="px-5 py-3">{t('servers.status')}</th><th class="px-5 py-3">{t('common.bungeeTokens')}</th><th class="px-5 py-3">{t('servers.actions')}</th></tr>
					</thead>
					<tbody class="divide-y divide-slate-800">
						{#each data.servers as server}
							<tr class="text-slate-300">
								<td class="px-5 py-4 font-medium text-white">{server.name}</td>
								<td class="px-5 py-4">{server.public_host || server.public_addr || t('servers.defaultRoute')}</td>
								<td class="px-5 py-4 font-mono text-xs text-slate-400">{server.upstream}</td>
								<td class="px-5 py-4 text-xs uppercase text-cyan-300">{server.forwarding_mode || t('common.globalFallback')}</td>
								<td class="px-5 py-4">{server.priority}</td>
								<td class="px-5 py-4"><span class={server.enabled ? 'text-emerald-400' : 'text-slate-500'}>{server.enabled ? t('servers.enabled') : t('servers.disabled')}</span><span class={`ml-2 ${serverHealthClass(data.serverHealth[server.id]?.status ?? 'unknown')}`}>{serverHealthLabel(data.serverHealth[server.id]?.status ?? 'unknown')}</span>{#if data.serverHealth[server.id]?.gate_registered !== undefined}<span class="ml-2 text-cyan-300">{serverGateLabel(data.serverHealth[server.id])}</span>{/if}</td>
								<td class="px-5 py-4">{#if server.bungeeguard_allowed_token}<details><summary class="cursor-pointer text-[11px] text-amber-200 hover:text-amber-100">{t('common.revealToken')}</summary><code class="mt-2 block break-all text-[11px] text-amber-300">{server.bungeeguard_allowed_token}</code></details>{:else}<span class="text-[11px] text-slate-500">{t('common.tokenHidden')}</span>{/if}{#if data.canManageBungeeGuardTokens}<form class="mt-2" method="POST" action="?/rotateBungeeGuardToken" onsubmit={(event) => { if (!confirm(t('common.rotateConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="text-[11px] text-amber-200 hover:text-amber-100">{t('action.regenerate')}</button></form>{/if}</td>
								<td class="flex gap-2 px-5 py-3">
									{#if canToggleServers}<form method="POST" action="?/setEnabled"><input type="hidden" name="id" value={server.id} /><input type="hidden" name="enabled" value={String(!server.enabled)} /><button class="rounded border border-slate-700 px-2 py-1 text-xs hover:border-cyan-400">{server.enabled ? t('servers.disable') : t('servers.enable')}</button></form>{/if}
									{#if canManageServers}<form method="POST" action="?/remove" onsubmit={(event) => { if (!confirm(t('common.deleteConfirm'))) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="rounded border border-red-900 px-2 py-1 text-xs text-red-300 hover:border-red-400">{t('action.delete')}</button></form>{/if}
								</td>
							</tr>
						{:else}
			<tr><td class="px-5 py-8 text-center text-slate-500" colspan="8">{t('servers.noServers')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="flex items-center justify-between border-t border-slate-800 px-5 py-3 text-xs text-slate-500"><span>{data.serverTotal ? `${data.serverOffset + 1}-${Math.min(data.serverOffset + data.serverLimit, data.serverTotal)} / ${data.serverTotal}` : '0'}</span><div class="flex gap-2">{#if data.serverHasPrevious}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=servers&server_q=${encodeURIComponent(data.serverSearch)}&server_enabled=${encodeURIComponent(data.serverEnabledFilter)}&server_offset=${Math.max(0, data.serverOffset - data.serverLimit)}`}>{t('servers.previous')}</a>{/if}{#if data.serverHasNext}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`/?view=servers&server_q=${encodeURIComponent(data.serverSearch)}&server_enabled=${encodeURIComponent(data.serverEnabledFilter)}&server_offset=${data.serverOffset + data.serverLimit}`}>{t('servers.next')}</a>{/if}</div></div>
			<div class="border-t border-slate-800 bg-slate-950/30 p-5">
				<p class="mb-3 text-xs uppercase tracking-wider text-slate-500">{t('scope.editRouting')}</p>
				<div class="grid gap-3 lg:grid-cols-2">
					{#each data.servers as server}
						{#if canManageServers}						<form method="POST" action="?/updateServer" class="grid gap-2 rounded-lg border border-slate-800 bg-slate-900/70 p-3 sm:grid-cols-2">
							<input type="hidden" name="id" value={server.id} />
							<input name="name" required value={server.name} aria-label={`${server.name} name`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="upstream" required value={server.upstream} aria-label={`${server.name} upstream`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white" />
							<input name="public_host" value={server.public_host ?? ''} placeholder={t('common.publicHostname')} aria-label={`${server.name} ${t('common.publicHostname')}`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="public_addr" value={server.public_addr ?? ''} placeholder={t('common.publicAddress')} aria-label={`${server.name} ${t('common.publicAddress')}`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white" />
							<select name="forwarding_mode" value={server.forwarding_mode ?? ''} aria-label={`${server.name} ${t('servers.forwarding')}`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-white"><option value="">{t('common.globalFallback')}</option><option value="none">{t('common.none')}</option><option value="legacy">{t('common.legacy')}</option><option value="bungeeguard">{t('common.bungee')}</option><option value="velocity">{t('common.velocity')}</option></select>
							<input name="ping_motd" maxlength="256" value={server.ping_motd ?? ''} placeholder={t('common.pingMotd')} aria-label={`${server.name} ${t('common.pingMotd')}`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="priority" type="number" min="0" value={server.priority} aria-label={`${server.name} priority`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">{t('scope.saveServer')} {server.name}</button>
						</form>{/if}
					{/each}
				</div>
			</div>
		</section>
		{/if}

		{#if data.view === 'audit'}
		<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="border-b border-slate-800 px-5 py-4">
						<div class="flex flex-wrap items-end justify-between gap-3"><div><h2 class="font-semibold text-white">{t('audit.title')}</h2><p class="mt-1 text-sm text-slate-400">{t('audit.help')}</p></div><form method="GET" class="grid gap-2 sm:grid-cols-[minmax(10rem,1fr)_auto_auto]"><input type="hidden" name="view" value="audit" /><label class="sr-only" for="audit-actor">{t('audit.actorSearch')}</label><input id="audit-actor" name="actor" value={data.auditActor} placeholder={t('audit.actorSearch')} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-slate-200 placeholder:text-slate-600" /><label class="sr-only" for="audit-event">{t('audit.title')}</label><select id="audit-event" name="event" aria-label={t('audit.title')} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-slate-200"><option value="" selected={!data.auditEvent}>{t('audit.all')}</option><option value="connect.blocked" selected={data.auditEvent === 'connect.blocked'}>{t('audit.blocked')}</option><option value="policy.update" selected={data.auditEvent === 'policy.update'}>{t('audit.policy')}</option><option value="geo.update" selected={data.auditEvent === 'geo.update'}>{t('audit.geo')}</option><option value="policy.reset" selected={data.auditEvent === 'policy.reset'}>{t('audit.reset')}</option><option value="server.create" selected={data.auditEvent === 'server.create'}>{t('audit.server')}</option><option value="rule.create" selected={data.auditEvent === 'rule.create'}>{t('audit.rule')}</option><option value="rule.delete" selected={data.auditEvent === 'rule.delete'}>{t('audit.ruleDelete')}</option><option value="server.update" selected={data.auditEvent === 'server.update'}>{t('audit.serverUpdate')}</option><option value="server.delete" selected={data.auditEvent === 'server.delete'}>{t('audit.serverDelete')}</option><option value="server.bungeeguard_token.rotate" selected={data.auditEvent === 'server.bungeeguard_token.rotate'}>{t('audit.tokenRotate')}</option></select><button class="rounded border border-slate-700 px-2 py-1.5 text-xs text-slate-300 hover:border-cyan-400">{t('action.refresh')}</button></form></div>
			</div>
			<div class="divide-y divide-slate-800">
				{#each data.audits as audit}
					<div class="grid gap-3 px-5 py-4 text-sm sm:grid-cols-[auto_minmax(0,1fr)]">
						<time class="pt-1 text-xs text-slate-500 sm:w-36" datetime={audit.created_at}>{formatDateTime(data.locale, audit.created_at)}</time>
						<div class="min-w-0 border-l-2 border-cyan-500/30 pl-4">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1"><span class="font-medium text-white">{auditEventLabel(audit.event)}</span>{#if audit.actor}<span class="text-cyan-300">{audit.actor}</span>{/if}{#if audit.server_id}<span class="text-slate-500">server #{audit.server_id}</span>{/if}</div>
							{#if Object.keys(audit.details ?? {}).length}<details class="mt-2"><summary class="cursor-pointer text-xs text-slate-500 hover:text-cyan-300">{t('audit.details')}</summary><dl class="mt-2 grid gap-x-4 gap-y-1 rounded-lg bg-slate-950/50 p-3 text-xs sm:grid-cols-2">{#each Object.entries(audit.details ?? {}) as [key, value]}<div class="min-w-0"><dt class="text-slate-500">{key}</dt><dd class="break-words font-mono text-slate-300">{auditValue(value)}</dd></div>{/each}</dl></details>{/if}
						</div>
					</div>
				{:else}
					<div class="px-5 py-8 text-center text-sm text-slate-500">{t('audit.empty')}</div>
				{/each}
			</div>
			<div class="flex items-center justify-between border-t border-slate-800 px-5 py-3 text-xs text-slate-500"><span>{t('audit.offset')} {formatNumber(data.locale, data.auditOffset)}</span><div class="flex gap-2">{#if data.auditOffset > 0}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`?view=audit&event=${encodeURIComponent(data.auditEvent)}&actor=${encodeURIComponent(data.auditActor)}&offset=${Math.max(0, data.auditOffset - data.auditLimit)}`}>{t('audit.previous')}</a>{/if}{#if data.auditHasNext}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`?view=audit&event=${encodeURIComponent(data.auditEvent)}&actor=${encodeURIComponent(data.auditActor)}&offset=${data.auditOffset + data.auditLimit}`}>{t('audit.next')}</a>{/if}</div></div>
		</section>
		<section class="mt-8 rounded-xl border border-slate-800 bg-slate-900/80 p-5"><div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="font-semibold text-white">{t('incident.title')}</h2><p class="mt-1 text-sm text-slate-400">{t('incident.help')}</p></div><span class="rounded-full bg-amber-400/10 px-3 py-1 text-xs text-amber-200">{data.incidentNotes.filter((incident) => incident.status === 'open').length} {t('incident.open')}</span></div>
			{#if form?.error}<div role="alert" aria-live="assertive" class="mt-4 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">{form.error}</div>{/if}{#if form?.success}<div role="status" aria-live="polite" class="mt-4 rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-300">{form.success}</div>{/if}
			{#if data.canManageIncidents}<form method="POST" action="?/createIncident" class="mt-4 grid gap-3 rounded-lg border border-slate-800 bg-slate-950/40 p-4"><input name="title" required maxlength="200" aria-label={t('incident.title')} placeholder={t('incident.title')} class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white placeholder:text-slate-600" /><textarea name="note" required maxlength="10000" rows="3" aria-label={t('incident.note')} placeholder={t('incident.note')} class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white placeholder:text-slate-600"></textarea><button class="justify-self-start rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">{t('incident.create')}</button></form>{/if}
			<div class="mt-4 space-y-3">{#each data.incidentNotes as incident}<article class="rounded-lg border border-slate-800 bg-slate-950/40 p-4"><div class="flex flex-wrap items-start justify-between gap-3"><div><h3 class="font-medium text-white">{incident.title}</h3><p class="mt-1 whitespace-pre-wrap break-words text-sm text-slate-300">{incident.note}</p><p class="mt-2 text-xs text-slate-500">{incident.actor} · {formatDateTime(data.locale, incident.updatedAt)}</p></div><div class="flex items-center gap-2"><span class={incident.status === 'open' ? 'rounded-full bg-amber-400/10 px-2 py-1 text-xs text-amber-200' : 'rounded-full bg-slate-700 px-2 py-1 text-xs text-slate-400'}>{incident.status === 'open' ? t('incident.open') : t('incident.closed')}</span>{#if data.canManageIncidents && incident.status === 'open'}<form method="POST" action="?/closeIncident"><input type="hidden" name="id" value={incident.id} /><button class="rounded border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:border-cyan-400">{t('incident.close')}</button></form>{/if}</div></div></article>{:else}<p class="py-6 text-center text-sm text-slate-500">{t('incident.empty')}</p>{/each}</div>
		</section>
		{/if}
	</div>
</main>
