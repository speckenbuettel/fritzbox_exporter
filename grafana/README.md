# Grafana dashboard working with this exporter

This dashboard is based on the following dashboard:
https://grafana.com/grafana/dashboards/713

Instead of InfluxDB it uses Prometheus and has been modified and enhanced.

The dashboard is now also published to [Grafana](https://grafana.com/grafana/dashboards/12579)

The dashboard is now also working with Grafana 7, for Grafana 6 use [Dashboard_for_grafana6.json](Dashboard_for_grafana6.json)

These inherited dashboards require query/label adjustments for the current
[5690 and 4040 profiles](../examples/dashboard/README.md) and diagnostic metrics.
