#!/usr/bin/env python3

import json
import re
import selectors
import subprocess
import urllib.error
import urllib.request

from collect import MAX_COMMAND_OUTPUT_BYTES, ReceiptError


START_PATTERN = re.compile(r"Starting to serve on 127\.0\.0\.1:([0-9]{1,5})\n?")


def _proxy_port(process):
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    try:
        if not selector.select(timeout=10):
            raise ReceiptError("kubectl proxy did not start within ten seconds")
        line = process.stdout.readline()
    finally:
        selector.close()
    match = START_PATTERN.fullmatch(line)
    if match is None or not 1 <= int(match.group(1)) <= 65535:
        raise ReceiptError("kubectl proxy did not report a bounded loopback port")
    return int(match.group(1))


def _response_json(response):
    body = response.read(MAX_COMMAND_OUTPUT_BYTES + 1)
    if len(body) > MAX_COMMAND_OUTPUT_BYTES:
        raise ReceiptError("server dry-run response exceeded the size limit")
    try:
        value = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ReceiptError("server dry-run response was not JSON") from error
    if not isinstance(value, dict):
        raise ReceiptError("server dry-run response was not an object")
    return value


def _stop_proxy(process):
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def post_server_dry_run(base, namespace, manifest, popen=subprocess.Popen,
                        opener=urllib.request.urlopen, port_reader=_proxy_port):
    command = [*base, "proxy", "--address=127.0.0.1", "--port=0"]
    try:
        process = popen(
            command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, bufsize=1,
        )
    except OSError as error:
        raise ReceiptError("kubectl proxy could not start") from error
    try:
        port = port_reader(process)
        path = f"/api/v1/namespaces/{namespace}/pods?dryRun=All"
        payload = json.loads(json.dumps(manifest))
        metadata = payload.setdefault("metadata", {})
        if metadata.get("namespace") not in {None, "", namespace}:
            raise ReceiptError("server dry-run object namespace does not match the request")
        metadata["namespace"] = namespace
        request = urllib.request.Request(
            f"http://127.0.0.1:{port}{path}",
            data=json.dumps(payload, separators=(",", ":")).encode(),
            headers={"Content-Type": "application/json"}, method="POST",
        )
        try:
            with opener(request, timeout=30) as response:
                return response.status, _response_json(response)
        except urllib.error.HTTPError as error:
            try:
                return error.code, _response_json(error)
            finally:
                error.close()
        except (OSError, TimeoutError) as error:
            raise ReceiptError("server dry-run request did not complete") from error
    finally:
        _stop_proxy(process)
