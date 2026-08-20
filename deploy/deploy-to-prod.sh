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
echo
echo "Scrape targets (credentials from deploy/vars/vault.yml):"
awk '/^\[agent\]/{in_agent=1; next} /^\[/{in_agent=0} in_agent && /ansible_host=/{sub(/^.*ansible_host=/, ""); sub(/ .*$/, ""); print "  http://<user>:<pass>@" $0 ":9144/metrics"}' deploy/hosts.ini