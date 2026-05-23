# Catalog

The catalog supplies default service metadata: icons, descriptions, primary
tags, tags, and canonical names. Sources fill in what they know. For example, a
Kubernetes HTTPRoute knows its hostname, but not that it is Grafana. The
registry fills the rest from the catalog by matching the service name.

Compass ships with ~180 entries baked in (see
[`internal/catalog/services.yaml`][catalog-source]) covering common
self-hosted apps. Every embedded icon is a [Dashboard Icons][dashboardicons]
slug. That project aggregates [selfh.st][selfhst] plus a wider set of
homelab logos, so a freshly discovered `grafana` HTTPRoute renders with the
right logo and a one-line description without any per-cluster configuration.

[catalog-source]: https://github.com/adinhodovic/compass/blob/main/internal/catalog/services.yaml

[dashboardicons]: https://dashboardicons.com/
[selfhst]: https://selfh.st/icons/

## Schema

```yaml
service-slug:
  description: Short blurb.
  icon: dashboardicons:service-slug
  primary_tag: observability
  tags: [observability, dashboards]
```

Slugs are normalized to lowercase alphanumeric for lookup, so `argo-cd`,
`Argo CD`, and `argocd` all match the same entry.

The embedded catalog uses [Dashboard Icons][dashboardicons] for self-hosted
software, with thanks to [selfh.st][selfhst] as the upstream source for many
of those logos. The legacy `selfhst:<name>` prefix still works as a
back-compat alias. Catalog overrides use the same `icon:` syntax as
services; see [Sources → Icon resolution](sources.md#icon-resolution).

## Overrides

```yaml
catalog:
  path: catalog/
```

`catalog.path` is a directory, resolved relative to `compass.yaml`. Every
`*.yaml` / `*.yml` file inside is loaded in lexical order and merged onto
the embedded base per field. A file that only sets `tags:` leaves
`description` and `icon` alone.

Splitting by concern keeps diffs readable:

```text
catalog/
├── 01-descriptions.yaml
├── 02-icons.yaml
└── 99-local.yaml          # local entries the embedded catalog doesn't cover
```

A later file's value wins on conflict. Slice fields (`tags`) are replaced
wholesale, not appended. Restate the whole list to add a tag to an
embedded entry.
