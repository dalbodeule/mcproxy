import { readFile } from 'node:fs/promises';

const packageJSON = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
const lockJSON = JSON.parse(await readFile(new URL('../package-lock.json', import.meta.url), 'utf8'));

const minimumMajors = {
	'@sveltejs/kit': 3,
	'@tailwindcss/vite': 4,
	tailwindcss: 4,
};

for (const [name, minimumMajor] of Object.entries(minimumMajors)) {
	const declared = packageJSON.devDependencies?.[name];
	const locked = lockJSON.packages?.[`node_modules/${name}`]?.version;
	const declaredMajor = Number(declared?.match(/\d+/)?.[0]);
	const lockedMajor = Number(locked?.match(/^\d+/)?.[0]);
	if (!Number.isInteger(declaredMajor) || declaredMajor < minimumMajor) {
		throw new Error(`${name} declares ${declared ?? 'no version'}; major ${minimumMajor}+ is required`);
	}
	if (!Number.isInteger(lockedMajor) || lockedMajor < minimumMajor) {
		throw new Error(`${name} resolves to ${locked ?? 'no locked version'}; major ${minimumMajor}+ is required`);
	}
	console.log(`${name}: declared ${declared}, locked ${locked}`);
}
