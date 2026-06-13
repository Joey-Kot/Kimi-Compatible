#!/bin/sh
# Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.

set -eu

if [ -x ./kimi-compatible ]; then
  exec ./kimi-compatible "$@"
fi

exec go run ./cmd/server "$@"
