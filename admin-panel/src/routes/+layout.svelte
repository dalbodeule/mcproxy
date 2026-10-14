<script lang="ts">
	import '../app.css';
	import type { LayoutData } from './$types';
	import { onMount } from 'svelte';
	import { translate } from '#lib/i18n.js';
	let { data, children }: { data: LayoutData; children: import('svelte').Snippet } = $props();
	onMount(() => { document.documentElement.lang = data.locale; });
</script>

<svelte:body class:high-contrast={data.contrast} />
<form method="POST" action="/contrast" class="fixed right-3 top-3 z-50">
	<input type="hidden" name="contrast" value={data.contrast ? 'normal' : 'high'} />
	<button type="submit" class="rounded-lg border border-slate-700 bg-slate-900/90 px-2.5 py-1.5 text-xs text-slate-300 shadow-lg hover:border-cyan-400 hover:text-cyan-200" aria-label={translate(data.locale, data.contrast ? 'common.contrastNormal' : 'common.contrastHigh')}>
		{data.contrast ? translate(data.locale, 'common.contrastNormal') : translate(data.locale, 'common.contrastHigh')}
	</button>
</form>
{@render children()}
