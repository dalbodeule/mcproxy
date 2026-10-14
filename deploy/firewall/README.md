# Host firewall example

`mcproxy.nft.example` is an optional first-line Linux host rule for limiting excessive new Minecraft TCP SYN packets per source address. `sysctl-mcproxy.conf.example` enables SYN cookies and shows capacity-dependent backlog/conntrack settings. These are examples, not a universal production firewall: tune the rate to the normal peak, merge them with the host's existing rules, and verify the provider's IPv4/IPv6 behavior before applying them.

Inspect and syntax-check it without changing the live firewall:

```sh
nft -c -f deploy/firewall/mcproxy.nft.example
```

Review the file first; `sysctl -p` applies the values, so run it only during a planned maintenance window:

```sh
sed -n '1,120p' deploy/firewall/sysctl-mcproxy.conf.example
sudo sysctl -p deploy/firewall/sysctl-mcproxy.conf.example
```

Use the distribution's documented persistent sysctl mechanism after review. Do not uncomment `nf_conntrack_max` or backlog values without checking memory and the provider/node limit.

Load it only after a maintenance/recovery path is confirmed:

```sh
sudo nft -f deploy/firewall/mcproxy.nft.example
nft list table inet mcproxy
```

This does not replace cloud L4 DDoS protection, SYN cookies, conntrack sizing, or an upstream load balancer. Do not expose the HTTP/admin port through this rule; keep `MCPROXY_HTTP_ADDR` private and use the separate panel proxy/VPN. Persist the final, reviewed rules using the host distribution's nftables service rather than relying on an interactive shell.
