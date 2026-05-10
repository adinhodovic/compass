---
title: Billing
order: 2
tags: [admin, billing]
---

# Billing

Where to look when something is charging us money:

| Provider   | Where                                                |
| ---------- | ---------------------------------------------------- |
| Cloudflare | Account → Billing → Subscriptions                    |
| Hetzner    | Cloud console → Billing                              |
| GitHub     | Org → Billing & licenses                             |
| Tailscale  | Admin console → Billing                              |

## Cost spikes

If the monthly total jumps by more than ~20%, check Grafana's "Cloud
spend" dashboard first; it tags spend by service.
