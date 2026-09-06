#!/usr/bin/env python3
"""Opt-in root Linux runtime regressions: python3 runtime_netmon.py /path/to/netmon."""
import http.client
import json
import pathlib
import socket
import subprocess
import sys
import tempfile
import time

binary = str(pathlib.Path(sys.argv[1]).resolve())

def request(host, port, path, method='GET'):
    connection = http.client.HTTPConnection(host, port, timeout=3)
    try:
        connection.request(method, path, body='{}' if method == 'POST' else None)
        response = connection.getresponse()
        return response.status, response.read().decode()
    finally:
        connection.close()

def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]

def configuration(number, address='127.0.0.1'):
    return f'''global:
  metrics_addr: {address}
  metrics_port: {number}
metadata:
  locations:
    path: locations.yaml
  roles:
    path: roles.yaml
  unknown:
    enabled: false
discovery:
  traceroute:
    enabled: false
connections:
  enabled: false
irq_affinity:
  enabled: false
bandwidth:
  enabled: true
  interfaces: [lo]
  interval: 100ms
logging:
  level: info
  format: json
'''

def ready(process, address, number):
    for _ in range(80):
        if process.poll() is not None:
            raise AssertionError('netmon exited before readiness')
        try:
            if request(address, number, '/ready')[0] == 200:
                return
        except OSError:
            pass
        time.sleep(.05)
    raise AssertionError('readiness timeout')

def stop(process):
    process.terminate()
    try:
        process.wait(5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()
        raise AssertionError('shutdown timeout')

with tempfile.TemporaryDirectory(prefix='netmon-runtime-regression.') as directory:
    work = pathlib.Path(directory)
    locations = work / 'locations.yaml'
    roles = work / 'roles.yaml'
    locations.write_text('locations:\n  - network: 127.0.0.0/8\n    location: original\n')
    good_roles = 'roles:\n  - network: 127.0.0.0/8\n    role: local\n'
    roles.write_text(good_roles)
    config = work / 'config.yaml'
    number = port()
    config.write_text(configuration(number).replace('traceroute:\n    enabled: false', 'traceroute:\n    enabled: true\n    interval: 0s'))
    result = subprocess.run([binary, '--config', str(config)], capture_output=True, text=True, timeout=5)
    assert result.returncode != 0 and 'panic:' not in result.stderr
    print('PASS invalid interval rejected without panic')
    for address in ('::1', '127.0.0.1'):
        number = port()
        original = configuration(number, address)
        config.write_text(original)
        with (work / 'netmon.log').open('w') as log:
            process = subprocess.Popen([binary, '--config', str(config)], stdout=log, stderr=log)
            try:
                ready(process, address, number)
                print('PASS HTTP bind', address)
                code, body = request(address, number, '/api/v1/monitoring')
                assert code == 200 and 'bandwidth' in json.loads(body)
                config.write_text(original.replace('locations.yaml', 'different.yaml'))
                code, body = request(address, number, '/api/v1/config/reload', 'POST')
                assert code >= 400 and 'restart' in body
                print('PASS changed configuration rejected with restart requirement')
                config.write_text(original)
                locations.write_text('locations:\n  - network: 127.0.0.0/8\n    location: changed\n  - network: 192.0.2.0/24\n    location: second\n')
                roles.write_text('roles: [invalid')
                assert request(address, number, '/api/v1/config/reload', 'POST')[0] >= 400
                code, body = request(address, number, '/api/v1/metadata/status')
                assert json.loads(body)['sources']['locations']['entries_count'] == 1
                roles.write_text(good_roles)
                assert request(address, number, '/api/v1/config/reload', 'POST')[0] == 200
                code, body = request(address, number, '/api/v1/metadata/status')
                assert json.loads(body)['sources']['locations']['entries_count'] == 2
                print('PASS failed metadata transaction preserves old data; valid transaction commits')
            finally:
                stop(process)
        locations.write_text('locations:\n  - network: 127.0.0.0/8\n    location: original\n')
