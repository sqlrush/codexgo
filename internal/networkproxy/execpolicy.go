package networkproxy

// ApplyExecPolicyNetworkRules overlays the compiled allow/deny domain lists
// produced by an exec policy (see execpolicy.Policy.CompiledNetworkDomains) onto
// a NetworkProxyConfig, returning the updated config.
//
// This mirrors Rust's `apply_exec_policy_network_rules`
// (core/src/network_proxy_loader.rs and core/src/config/network_proxy_spec.rs):
// exec-policy rules are upserted AFTER the config-sourced domain entries, so for
// a given host the exec-policy disposition wins over any config-sourced entry
// for the same normalized host. Allowed hosts are upserted as Allow and denied
// hosts as Deny; each incoming list is de-duplicated on its normalized host so a
// host appearing twice only upserts once (matching the Rust `HashSet` guard).
func ApplyExecPolicyNetworkRules(config NetworkProxyConfig, allowedDomains, deniedDomains []string) NetworkProxyConfig {
	config.Network = upsertNetworkDomains(config.Network, allowedDomains, DomainPermissionAllow)
	config.Network = upsertNetworkDomains(config.Network, deniedDomains, DomainPermissionDeny)
	return config
}

// upsertNetworkDomains upserts each host in hosts with the given permission,
// skipping repeats of the same verbatim host already applied in this call. This
// mirrors Rust's `HashSet<String>` guard, which de-duplicates on the verbatim
// host string (not its normalized form); WithUpsertDomainPermission then
// collapses entries that share a normalized host, keeping the latest.
func upsertNetworkDomains(network NetworkProxySettings, hosts []string, permission NetworkDomainPermission) NetworkProxySettings {
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		network = network.WithUpsertDomainPermission(host, permission)
	}
	return network
}
