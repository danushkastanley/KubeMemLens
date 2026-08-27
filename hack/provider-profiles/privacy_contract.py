#!/usr/bin/env python3

import ipaddress
import re


FORBIDDEN_KEYS = {
    "account",
    "accountid",
    "cluster",
    "clustername",
    "containerid",
    "context",
    "error",
    "kubeconfig",
    "logs",
    "namespace",
    "nodename",
    "nodeuid",
    "podname",
    "poduid",
    "project",
    "projectid",
    "providerid",
    "rawerror",
    "rawlogs",
    "resourcegroup",
    "subscription",
    "subscriptionid",
    "token",
}
SENSITIVE_TOKEN_PATTERN = re.compile(
    r"(?:^|[-_ .:/])(?:account|cluster|context|kubeconfig|namespace|password|project|"
    r"resource[-_ ]?group|secret|subscription|tenant|token)(?:$|[-_ .:/])",
    re.IGNORECASE,
)
RESOURCE_PATTERN = re.compile(
    r"(?:arn:(?:aws|aws-cn|aws-us-gov):|/(?:projects|subscriptions)/|"
    r"(?:aws|azure|gce)://|https?://|s3://|gs://)",
    re.IGNORECASE,
)
EMAIL_PATTERN = re.compile(r"(?<![A-Za-z0-9._%+-])[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}(?![A-Za-z0-9.-])")
CREDENTIAL_PATTERN = re.compile(
    r"(?:AKIA|ASIA)[A-Z0-9]{16}|-----BEGIN [A-Z ]+PRIVATE KEY-----|"
    r"\bBearer\s+[A-Za-z0-9._~+/-]+=*|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}"
    r"|\b(?:password|secret|token)\s*[:=]\s*\S+",
    re.IGNORECASE,
)
IPV4_PATTERN = re.compile(r"(?<![0-9.])(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?![0-9.])")
IPV6_CANDIDATE_PATTERN = re.compile(r"(?<![0-9A-Fa-f:])\[?[0-9A-Fa-f:]{2,}\]?(?![0-9A-Fa-f:])")
ACCOUNT_ID_PATTERN = re.compile(r"(?<![A-Za-z0-9])[0-9]{12}(?![A-Za-z0-9])")
UUID_PATTERN = re.compile(
    r"(?<![A-Fa-f0-9])[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[1-5][A-Fa-f0-9]{3}-"
    r"[89ABab][A-Fa-f0-9]{3}-[A-Fa-f0-9]{12}(?![A-Fa-f0-9])"
)


def normalise_key(key):
    return re.sub(r"[^a-z0-9]", "", key.casefold())


def contains_ip_address(value):
    for candidate in IPV4_PATTERN.findall(value):
        try:
            ipaddress.ip_address(candidate)
            return True
        except ValueError:
            pass
    for candidate in IPV6_CANDIDATE_PATTERN.findall(value):
        candidate = candidate.strip("[]")
        if ":" not in candidate:
            continue
        try:
            ipaddress.ip_address(candidate)
            return True
        except ValueError:
            pass
    return False


def reject_sensitive_content(value, error_type):
    if isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str):
                raise error_type("evidence object keys must be strings")
            if normalise_key(key) in FORBIDDEN_KEYS:
                raise error_type(f"evidence contains forbidden key: {key}")
            reject_sensitive_content(child, error_type)
        return
    if isinstance(value, list):
        for child in value:
            reject_sensitive_content(child, error_type)
        return
    if not isinstance(value, str):
        return
    if RESOURCE_PATTERN.search(value):
        raise error_type("evidence contains a provider resource path or URL")
    if EMAIL_PATTERN.search(value):
        raise error_type("evidence contains an email address")
    if CREDENTIAL_PATTERN.search(value):
        raise error_type("evidence contains credential-like text")
    if ACCOUNT_ID_PATTERN.search(value) or UUID_PATTERN.search(value):
        raise error_type("evidence contains an account or subscription identifier")
    if SENSITIVE_TOKEN_PATTERN.search(value):
        raise error_type("evidence contains an identifier-bearing token")
    if contains_ip_address(value):
        raise error_type("evidence contains an IP address")
