# Copyright 2020-2022 JackTrip Labs, Inc.
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

local:
	@go build -o jacktrip-agent ./cmd

agent-amd64:
	@docker buildx build --platform linux/amd64 --target=artifact --output type=local,dest=./ .

agent-arm:
	@docker buildx build --platform linux/arm/v7 --target=artifact --output type=local,dest=./ .

fmt:
	@gofmt -l -w `find ./ -name "*.go"`

lint:
	@golint ./...

# You need to disable root user logic in cmd/main.go
run_server:
	@go run ./cmd -s

ssh:
	@sshpass -p jacktrip ssh pi@jacktrip.local

update_device: agent-arm
	@mv jacktrip-agent-arm jacktrip-agent
	@echo 'built a jacktrip-agent binary'
	@sshpass -p jacktrip ssh pi@jacktrip.local "sudo mount -o remount,rw / && sudo rm /usr/local/bin/jacktrip-agent" || true
	@echo 'removed jacktrip-agent binary from the device'
	@sshpass -p jacktrip scp jacktrip-agent pi@jacktrip.local:~
	@echo 'copied local jacktrip-agent binary into the device'
	@sshpass -p jacktrip ssh pi@jacktrip.local "sudo mount -o remount,rw / && sudo mv ~/jacktrip-agent /usr/local/bin/jacktrip-agent && sudo systemctl restart jacktrip-agent.service"
	@echo 'restarted the service'
	
small-tests:
	@go clean -testcache
	@mkdir -p artifacts
	@gotestsum -f standard-verbose --junitfile artifacts/results-small.xml -- -coverprofile=artifacts/coverage.out -tags=unit ./...
