#!/bin/sh
# Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.

set -eu

if [ "$#" -eq 0 ] || [ "${1#-}" != "$1" ]; then
  if [ -n "${DEBUG_LOG_BODY:-}" ]; then
    set -- "--debug-log-body=${DEBUG_LOG_BODY}" "$@"
  fi
  if [ -n "${VERIFY_SSL:-}" ]; then
    set -- "--verify-ssl=${VERIFY_SSL}" "$@"
  fi
  if [ -n "${KIMI_HTTP_TIMEOUT:-}" ]; then
    set -- "--kimi-http-timeout" "${KIMI_HTTP_TIMEOUT}" "$@"
  fi
  if [ -n "${KIMI_MAX_IDLE_CONNS:-}" ]; then
    set -- "--kimi-max-idle-conns" "${KIMI_MAX_IDLE_CONNS}" "$@"
  fi
  if [ -n "${KIMI_MAX_IDLE_CONNS_PER_HOST:-}" ]; then
    set -- "--kimi-max-idle-conns-per-host" "${KIMI_MAX_IDLE_CONNS_PER_HOST}" "$@"
  fi
  if [ -n "${KIMI_MAX_CONNS_PER_HOST:-}" ]; then
    set -- "--kimi-max-conns-per-host" "${KIMI_MAX_CONNS_PER_HOST}" "$@"
  fi
  if [ -n "${READ_HEADER_TIMEOUT:-}" ]; then
    set -- "--read-header-timeout" "${READ_HEADER_TIMEOUT}" "$@"
  fi
  if [ -n "${IDLE_TIMEOUT:-}" ]; then
    set -- "--idle-timeout" "${IDLE_TIMEOUT}" "$@"
  fi
  if [ -n "${KIMI_MODELS:-}" ]; then
    set -- "--kimi-models" "${KIMI_MODELS}" "$@"
  fi
  if [ -n "${KIMI_MODEL:-}" ]; then
    set -- "--kimi-model" "${KIMI_MODEL}" "$@"
  fi
  if [ -n "${KIMI_BASE_URL:-}" ]; then
    set -- "--kimi-base-url" "${KIMI_BASE_URL}" "$@"
  fi
  if [ -n "${KIMI_API_KEY:-}" ]; then
    set -- "--kimi-api-key" "${KIMI_API_KEY}" "$@"
  fi
  if [ -n "${API_TOKEN:-}" ]; then
    set -- "--api-token" "${API_TOKEN}" "$@"
  fi
  if [ -n "${LISTEN:-}" ]; then
    set -- "--listen" "${LISTEN}" "$@"
  fi

  set -- kimi-compatible "$@"
fi

exec "$@"
