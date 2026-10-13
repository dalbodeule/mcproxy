export type Stats = {
	uptime_seconds: number;
	servers: number;
	enabled_servers: number;
	policies: number;
	rules: number;
	counters: number;
	blocks: number;
};

export type Server = {
	id: number;
	name: string;
	public_host?: string;
	public_addr?: string;
	upstream: string;
	forwarding_mode: string;
	ping_motd?: string;
	priority: number;
	enabled: boolean;
	bungeeguard_allowed_token: string;
};

export type Audit = {
	id: number;
	event: string;
	actor?: string;
	server_id?: number;
	details: Record<string, unknown>;
	created_at: string;
};

export type Policy = {
	id?: number;
	ip_burst_10s: number;
	ip_burst_60s: number;
	name_burst_10s: number;
	name_burst_60s: number;
	deny_first: boolean;
	kick_message: string;
	geo_mode: string;
	geo_list: string[];
};

export type GeoPolicy = {
	mode: string;
	countries: string[];
};

export type Rule = {
	id: number;
	kind: string;
	target: string;
	is_cidr: boolean;
	reason?: string;
	enabled: boolean;
	expires_at?: string;
	server_id?: number;
};
