.score >= 7 and
any(.checks[]; .name == "Branch-Protection" and .score >= 4) and
any(.checks[]; .name == "Token-Permissions" and .score >= 8) and
any(.checks[]; .name == "Dangerous-Workflow" and .score >= 8) and
any(.checks[]; .name == "Pinned-Dependencies" and .score >= 8) and
any(.checks[]; .name == "Signed-Releases" and .score >= 8) and
any(.checks[]; .name == "Vulnerabilities" and .score >= 7)
