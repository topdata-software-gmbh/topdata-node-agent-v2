#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f deploy/vars/vault.yml ]; then
    echo "Error: deploy/vars/vault.yml not found." >&2
    echo "Create it once with: ansible-vault create deploy/vars/vault.yml" >&2
    echo "(keys: auth_username, auth_password, shops_root — see deploy/vars/vault.example.yml)" >&2
    exit 1
fi

mkdir -p deploy/bin

GOOS=linux GOARCH=arm64 go build -o deploy/bin/topdata-agent-arm64 .
GOOS=linux GOARCH=amd64 go build -o deploy/bin/topdata-agent-amd64 .

ansible-playbook -i deploy/hosts.ini deploy/playbook-deploy.yaml --ask-vault-pass "$@"

echo "Deployed topdata-agent to all hosts in deploy/hosts.ini."
echo "Smoke check: curl -u <user>:<pass> http://<host>:9144/metrics"