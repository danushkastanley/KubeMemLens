"""Network defence in depth; certificates and admission remain authoritative."""
from admission_resources import resource


def policies(namespace):
    api = "networking.k8s.io/v1"
    return [
        resource("NetworkPolicy", "admission-api", namespace, api, spec={
            "podSelector": {"matchLabels": {"app": "admission-api"}},
            "policyTypes": ["Ingress"],
            # Match the standard aggregated API policy: control-plane source
            # ranges vary by installation; only the proxy certificate grants
            # access to this TLS listener.
            "ingress": [{"ports": [{"protocol": "TCP", "port": 8443}]}],
        }),
        resource("NetworkPolicy", "binding-node", namespace, api, spec={
            "podSelector": {"matchLabels": {"app": "binding-node"}},
            "policyTypes": ["Ingress", "Egress"],
            "ingress": [{
                "from": [{"podSelector": {"matchLabels": {"app": "admission-api"}}}],
                "ports": [{"protocol": "TCP", "port": 9443}],
            }],
            # Baseline and filesystem resolution are local operations. Return
            # traffic on permitted incoming connections is statefully allowed.
            "egress": [],
        }),
    ]
