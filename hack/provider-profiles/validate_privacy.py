#!/usr/bin/env python3

import argparse
import json
import sys
from pathlib import Path

from privacy_contract import reject_sensitive_content


def main():
    parser = argparse.ArgumentParser(description="Reject sensitive provider qualification evidence.")
    parser.add_argument("input")
    arguments = parser.parse_args()
    try:
        with Path(arguments.input).open(encoding="utf-8") as source:
            value = json.load(source)
        reject_sensitive_content(value, ValueError)
    except (OSError, json.JSONDecodeError, ValueError) as error:
        print(f"provider privacy error: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
