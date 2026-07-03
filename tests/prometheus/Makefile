.PHONY: start-prometheus metrics_gathered

# Download prometheus binary and put it on the path

start-prometheus:
	prometheus --config.file=prometheus.yml --storage.tsdb.path=data/

# This can be used to see what metrics have been scraped by Prometheus
# EvalHub prometheus server is running on port 8081
metrics_gathered:
	curl -s http://localhost:8081/metrics
