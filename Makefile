# Copyright 2020 20hz, LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: all agent fmt lint

all: lint fmt agent-amd64 agent-arm

agent-amd64:
	@GOOS=linux GOARCH=amd64 go build -o jacktrip-agent-amd64 ./cmd

agent-arm:
	@GOOS=linux GOARCH=arm go build -o jacktrip-agent-arm ./cmd

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...

small-tests:
	@go clean -testcache
	@mkdir -p artifacts
	@gotestsum -f standard-verbose --junitfile artifacts/results-small.xml -- -coverprofile=artifacts/coverage.out -tags=unit ./...
