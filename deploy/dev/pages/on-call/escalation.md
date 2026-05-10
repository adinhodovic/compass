---
title: Escalation Path
order: 1
tags: [on-call, incident]
---

# Escalation path

This page lives under `deploy/dev/pages/on-call/` (no numeric prefix), so it
lands in an "On Call" dropdown alongside the "Administration" one.

1. Page the primary on-call (PagerDuty primary rotation).
2. After 10 minutes without ack, page the secondary.
3. After 20 minutes without ack, page the engineering manager.
4. For SEV-1, also notify the comms channel `#incidents`.

## Quick actions

- Open Grafana → "Service health" board.
- Open the runbook for the failing service from its detail page.
- Snapshot the alert payload into the incident timeline.

## Monitoring stack

Live links to anything tagged `monitoring`, rendered by
`{{</* services tag=monitoring */>}}`:

{{< services tag=monitoring >}}
