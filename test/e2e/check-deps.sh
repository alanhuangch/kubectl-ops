#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

configure_docker
require_command "${KIND}"
require_command "${KUBECTL}"
require_command "${DOCKER}"
require_command "${JQ}"
require_command go

"${DOCKER}" info >/dev/null
echo "E2E dependencies are available."
