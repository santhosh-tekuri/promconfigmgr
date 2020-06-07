# promconfigmgr
generate prometheus configuration from kubernetes configmaps

use docker image `santhoshkt/promconfigmgr:1.0.1`
- image is build using `scratch` image
- command is `/promconfigmgr`
- takes two arguments
  1. location of prometheus.yml
     - should contain all configuration except `scrape_configs` and `rule_files`
  2. target directory
     - prometheus.yml and rule files are generated into this directory
- it watches for configMap changes with annotation `prometheus.io/config: "true"`
- on any change:
  - it generates prometheus.yml and rule files
  - does reload using `POST http://localhost:9090/-/reload"
  
`promconfigmgr` is supposed to be run as sidecar in same pod as prometheus server

below is sample statefulset:
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: prometheus
spec:
  serviceName: prometheus-headless
  replicas: 1
  volumeClaimTemplates:
  - metadata:
      name: datadir
    spec:
      accessModes: [ReadWriteOnce]
      resources:
        requests:
          storage: 8Gi
  template:
    spec:
      terminationGracePeriodSeconds: 300
      serviceAccountName: prometheus
      securityContext:
        fsGroup: 65534
        runAsGroup: 65534
        runAsUser: 65534
      volumes:
      - name: config
        configMap:
          name: prometheus
      - name: configdir
        emptyDir: {}
      containers:
      - name: configmgr
        image: santhoshkt/promconfigmgr:1.0.0
        args:
        - /tmp/config/prometheus.yml
        - /etc/prometheus/config
        volumeMounts:
        - name: config
          mountPath: /tmp/config
        - name: configdir
          mountPath: /etc/prometheus/config
      - name: prometheus
        image: prom/prometheus:v2.18.0
        ports:
        - name: http
          containerPort: 9090
        command:
        - sh
        - -ce
        - |
          if [ ! -f /etc/prometheus/config/prometheus.yml ]; then
              cp /tmp/config/prometheus.yml /etc/prometheus/config
          fi
          exec "$@"
        - --
        args:
        - /bin/prometheus
        - --config.file=/etc/prometheus/config/prometheus.yml
        - --storage.tsdb.path=/data
        - --storage.tsdb.retention.time=15d
        - --web.console.libraries=/etc/prometheus/console_libraries
        - --web.console.templates=/etc/prometheus/consoles
        - --web.enable-lifecycle
        volumeMounts:
        - name: config
          mountPath: /tmp/config
        - name: configdir
          mountPath: /etc/prometheus/config
        - name: datadir
          mountPath: /data
        readinessProbe:
          initialDelaySeconds: 30
          timeoutSeconds: 30
          httpGet:
            path: /-/ready
            port: 9090
        livenessProbe:
          initialDelaySeconds: 30
          timeoutSeconds: 30
          httpGet:
            path: /-/healthy
            port: 9090
```

sample configmap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-state-metrics-prometheus
  annotations:
    prometheus.io/config: "true"
data:
  prometheus.yml: |
    scrape_configs:
    - job_name: kube-state-metrics
      kubernetes_sd_configs:
      - role: service
      relabel_configs:
      - source_labels: [__meta_kubernetes_namespace, __meta_kubernetes_service_label_app]
        regex: monitoring;kube-state-metrics
        action: keep
  alerting_rules.yml: |
    groups:
    - name: kubernetes-apps
      rules:
      - alert: KubeJobFailed
        annotations:
          message: "Job {{ $labels.namespace }}/{{ $labels.job_name }} failed to complete."
        expr: kube_job_failed{job="kube-state-metrics"}  > 0
        for: 15m
        labels:
          severity: warning
```
