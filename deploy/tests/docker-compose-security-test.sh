#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

check_application_security_opt() {
  file=$1
  count=$(
    awk '
      $0 == "  sub2api:" {
        in_application = 1
        next
      }
      in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
        in_application = 0
      }
      in_application && $0 == "    security_opt:" {
        in_security_opt = 1
        next
      }
      in_application && in_security_opt && $0 == "      - no-new-privileges:true" {
        count++
      }
      END { print count + 0 }
    ' "$file"
  )

  if [ "$count" -ne 1 ]; then
    printf '%s must enable no-new-privileges exactly once for the sub2api service\n' "$file" >&2
    exit 1
  fi
}

check_fixed_proxy_network() {
  file=$1

  expected_lines=$(awk '
    $0 == "networks:" { in_networks = 1 }
    in_networks && $0 == "  sub2api-network:" { in_application_network = 1 }
    in_application_network && $0 == "    driver: bridge" { bridge++ }
    in_application_network && $0 == "    ipam:" { ipam++ }
    in_application_network && $0 == "      config:" { config++ }
    in_application_network && $0 == "        - subnet: 172.30.18.0/29" { subnet++ }
    in_application_network && $0 == "          gateway: 172.30.18.1" { gateway++ }
    END {
      printf "%d %d %d %d %d", bridge + 0, ipam + 0, config + 0, subnet + 0, gateway + 0
    }
  ' "$file")
  if [ "$expected_lines" != "1 1 1 1 1" ]; then
    printf '%s must pin sub2api-network to 172.30.18.0/29 via gateway 172.30.18.1\n' "$file" >&2
    exit 1
  fi

  trusted='      - SERVER_TRUSTED_PROXIES=${SERVER_TRUSTED_PROXIES:-172.30.18.1/32}'
  trusted_count=$(grep -Fxc "$trusted" "$file" || true)
  key_count=$(grep -Ec '^[[:space:]]*-[[:space:]]*SERVER_TRUSTED_PROXIES([[:space:]]*=.*)?[[:space:]]*$' "$file" || true)
  if [ "$trusted_count" -ne 1 ] || [ "$key_count" -ne 1 ]; then
    printf '%s must trust only the fixed gateway /32 exactly once\n' "$file" >&2
    exit 1
  fi
}

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  check_application_security_opt "$compose_file"
done

for compose_file in deploy/docker-compose.yml deploy/docker-compose.local.yml; do
  check_fixed_proxy_network "$compose_file"
done

if [ "$(grep -Fxc 'SERVER_TRUSTED_PROXIES=172.30.18.1/32' deploy/.env.example || true)" -ne 1 ] || \
   [ "$(grep -Ec '^SERVER_TRUSTED_PROXIES=' deploy/.env.example || true)" -ne 1 ]; then
  printf 'deploy/.env.example must configure only the fixed gateway /32 as its active trusted proxy\n' >&2
  exit 1
fi

printf 'docker compose security test passed\n'
