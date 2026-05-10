# Demo

A quick visual tour of Compass using the local development stack.

## Services Dashboard

The services dashboard is the primary operator view: discovered services,
source-aware grouping, filters, pinned services, recent services, and quick
actions in one place.

![Compass services dashboard](assets/images/services-landing.png)

The same dashboard supports theme switching without changing the underlying
server-rendered UI.

![Compass services dashboard in dark mode](assets/images/services-landing-dark.png)

Search narrows the registry quickly and keeps filters, grouping, and sort
controls close to the results.

![Compass services dashboard search results](assets/images/services-search.png)

Each service has a detail page with its URL, metadata, tags, embedded
Grafana panels, and backlinks to any markdown pages that reference it.

![Compass service detail page with metadata and panels](assets/images/services-detail.png)

## Pages

Compass can render markdown pages alongside the dashboard. Use them for
runbooks, architecture notes, on-call docs, and other operator context.

![Compass pages landing view](assets/images/pages-landing.png)

Pages can also embed live service cards and Grafana panels so documentation
stays connected to the currently discovered registry.

![Compass page with service cards and an embedded Grafana panel](assets/images/pages-features.png)

## Debug

The debug page shows per-source health and the services each source returned.
It is useful when a service is missing, duplicated, or coming from an
unexpected source.

![Compass debug page showing source health and discovered services](assets/images/debug.png)
