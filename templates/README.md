# RHOBS Synthetics Agent OpenShift Template

This OpenShift template deploys the RHOBS Synthetics Agent on an OpenShift cluster.

## Prerequisites

- OpenShift cluster
- Cluster administrator access (for ClusterRole/ClusterRoleBinding)
- Probe Custom Resource Definitions installed (`monitoring.rhobs/v1` or `monitoring.coreos.com/v1`)

## Installation

### Quick Installation
```bash
# Process and create from template with default parameters
oc process -f templates/template.yaml | oc apply -f -

# Install in specific namespace
oc process -f templates/template.yaml -p NAMESPACE=my-monitoring-namespace | oc apply -f -

# Install with parameter file
oc process -f templates/template.yaml --param-file=templates/example-parameters.env | oc apply -f -
```

### Custom Installation
```bash
# Process template with custom parameters
oc process -f templates/template.yaml \
  -p APPLICATION_NAME=my-synthetics-agent \
  -p NAMESPACE=my-monitoring-namespace \
  -p LOG_LEVEL=debug \
  | oc apply -f -
```

## Configuration

### Template Parameters

The following table lists the most important configurable parameters:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `NAMESPACE` | Namespace where the agent will be deployed | `"rhobs"` |
| `API_URLS` | YAML list of complete API URLs to poll for probes | Single example URL |
| `LABEL_SELECTOR` | Label selector for filtering probes | `"private=false,rhobs-synthetics/status=pending"` |
| `PROBE_NAMESPACE` | Namespace where probe resources will be created | `"rhobs"` |
| `POLLING_INTERVAL` | How often to poll APIs | `"30s"` |
| `LOG_LEVEL` | Log level (debug, info, warn, error) | `"info"` |
| `REPLICA_COUNT` | Number of replicas | `"1"` |
| `CPU_LIMIT` / `MEMORY_LIMIT` | Resource limits | `500m` / `512Mi` |

### Namespace Configuration

The template supports two different namespace parameters:

- **`NAMESPACE`** - Controls where the agent itself (ServiceAccount, ConfigMap, Deployment) is deployed
- **`PROBE_NAMESPACE`** - Controls where the agent creates Probe Custom Resources

These can be the same or different depending on your requirements:

```bash
# Deploy agent and create probes in the same namespace
oc process -f templates/template.yaml \
  -p NAMESPACE=monitoring \
  -p PROBE_NAMESPACE=monitoring \
  | oc apply -f -

# Deploy agent in one namespace, create probes in another
oc process -f templates/template.yaml \
  -p NAMESPACE=rhobs-agent \
  -p PROBE_NAMESPACE=openshift-monitoring \
  | oc apply -f -
```

### Parameter Files

Use the provided parameter files for different environments:

#### Development Configuration
```bash
# Copy and customize the development parameters
cp templates/example-parameters.env templates/my-dev-params.env
# Edit templates/my-dev-params.env with your values
oc process -f templates/template.yaml --param-file=templates/my-dev-params.env | oc apply -f -
```

#### Custom API Configuration
```bash
# For multiple API URLs, use YAML format in the parameter file:
API_URLS=|2+

  - "https://api1.example.com/api/metrics/v1/tenant1/probes"
  - "https://api2.example.com/api/metrics/v1/tenant2/probes"
  - "https://api3.example.com/api/metrics/v1/tenant3/probes"
```

## Required Permissions

The agent requires the following permissions:

- **Probe CRDs**: `get`, `list`, `watch`, `create`, `update`, `patch`, `delete`
- **CRDs**: `get`, `list` (to detect available API groups)
- **Namespaces**: `get`, `list`

These are automatically configured through the included RBAC resources in the template.

## Monitoring and Observability

### Health Checks

The agent exposes health endpoints on port 8080 (when implemented):
- `/health` - Liveness probe endpoint
- `/ready` - Readiness probe endpoint

### Logs

Configure logging via parameters:
```bash
oc process -f templates/template.yaml \
  -p LOG_LEVEL=debug \
  -p LOG_FORMAT=json \
  | oc apply -f -
```

View logs:
```bash
# View current logs
oc logs deployment/rhobs-synthetics-agent

# Follow logs
oc logs -f deployment/rhobs-synthetics-agent

# View logs from specific pod
oc logs -l app.kubernetes.io/name=rhobs-synthetics-agent
```

## Troubleshooting

### Common Issues

1. **Probe CRDs not found**
   ```bash
   oc get crd probes.monitoring.rhobs probes.monitoring.coreos.com
   ```

2. **Permission denied creating probes**
   ```bash
   oc auth can-i create probes.monitoring.rhobs --as=system:serviceaccount:openshift-monitoring:rhobs-synthetics-agent
   ```

3. **API connection issues**
   Check the agent logs and verify API URLs and authentication.

### Debug Mode

Enable debug logging:
```bash
oc process -f templates/template.yaml -p LOG_LEVEL=debug | oc apply -f -
```

### Checking Agent Status

```bash
# Check pod logs
oc logs -l app.kubernetes.io/name=rhobs-synthetics-agent

# Check created probes
oc get probes -A

# Describe the deployment
oc describe deployment rhobs-synthetics-agent

# Check template processing
oc process -f templates/template.yaml --param-file=templates/example-parameters.env
```

## Uninstalling

```bash
# Delete all resources created by the template
oc process -f templates/template.yaml | oc delete -f -

# Or delete by labels
oc delete all,configmap,serviceaccount,clusterrole,clusterrolebinding -l template=rhobs-synthetics-agent
```

Note: This will not remove the Probe Custom Resources that were created by the agent.

## Development

### Testing Template

```bash
# Process template without applying
oc process -f templates/template.yaml --param-file=templates/example-parameters.env

# Validate template syntax
oc process -f templates/template.yaml --param-file=templates/example-parameters.env --dry-run=client

# Test with different parameters
oc process -f templates/template.yaml -p REPLICA_COUNT=2 -p LOG_LEVEL=debug
```

### Adding to OpenShift Catalog

```bash
# Create template in openshift namespace to make it available cluster-wide
oc create -f templates/template.yaml -n openshift
```
