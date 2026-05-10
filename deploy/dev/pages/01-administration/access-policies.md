---
title: Access Policies
order: 1
tags: [security, governance]
---

# Access policies

Lives under `deploy/dev/pages/01-administration/`. The `01-` prefix on the
directory name controls section ordering in the navbar — lower numbers appear
first. The prefix is stripped from the displayed title.

## Roles

- **Owner** — full read/write on configuration and source secrets.
- **Operator** — can view services and edit static config, but not source
  credentials.
- **Viewer** — read-only.

## Onboarding

1. Add the user to the SSO group.
2. Add their identity to `catalog/access.yaml` if they need a personalized
   landing page.
3. Run through the on-call rotation runbook with them.
