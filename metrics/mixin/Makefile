# This Makefile should be used by developers to test generation of new new rules and alerts
JSONNET_FMT := jsonnetfmt -n 2 --max-blank-lines 2 --string-style s --comment-style s

all: prometheus_alert_rules.yaml prometheus_alert_rules_external.yaml lint

fmt:
	find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		xargs -n 1 -- $(JSONNET_FMT) -i

prometheus_alert_rules.yaml: mixin.libsonnet lib/alerts.jsonnet alerts/*.libsonnet
	jsonnet -S lib/alerts.jsonnet > $@

prometheus_alert_rules_external.yaml: mixin-external.libsonnet lib/alerts-external.jsonnet alerts/*.libsonnet
	jsonnet -S lib/alerts-external.jsonnet > $@

prometheus_rules.yaml: mixin.libsonnet lib/rules.jsonnet rules/*.libsonnet
	jsonnet -S lib/rules.jsonnet > $@

lint: prometheus_alert_rules.yaml
	find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		while read f; do \
			$(JSONNET_FMT) "$$f" | diff -u "$$f" -; \
		done

	promtool check rules prometheus_alert_rules.yaml

clean:
	rm -rf prometheus_alert_rules.yaml prometheus_alert_rules_external.yaml

