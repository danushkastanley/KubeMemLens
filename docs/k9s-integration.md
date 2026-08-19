# K9s Integration

KubeMemLens ships an optional read-only K9s plugin at `deploy/k9s/plugins.yaml`. In the Pod view, `Shift-M` runs the text explanation for the selected Pod using K9s's current namespace, context, and kubeconfig.

K9s loads a complete plugin collection from `$XDG_CONFIG_HOME/k9s/plugins.yaml` and also scans plugin directories under the XDG config/data locations. Merge the `kube-memlens-explain` entry into an existing collection rather than replacing other plugins. The current plugin contract and search paths are documented by [K9s](https://k9scli.io/topics/plugins/).

Prerequisites:

- `kubectl memlens version` succeeds on the same machine as K9s.
- The active user can access the KubeMemLens collector Service through Kubernetes service-proxy RBAC.
- K9s and `kubectl memlens` use the same context.

The plugin does not mutate Kubernetes resources, start a shell, or run in the background. K9s labels its plugin interface experimental, so validate the snippet after upgrading K9s.
