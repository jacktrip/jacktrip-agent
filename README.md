# JackTrip Virtual Studio Agent

This repository includes the golang source code used to build the
jacktrip-agent executable. This agent is used on both Virtual Studio devices
and servers. It communicates with the back-end REST APIs to retrieve
configuration, and interacts with systemd to manage various system processes.

This now uses docker buildx containers to build for multiple architectures. On
Debian/Ubuntu, you need to install these packages:

sudo apt-get install -y qemu qemu-user-static docker-ce

You'll also need to to run this to execute the registration scripts:

docker run --rm --privileged multiarch/qemu-user-static --reset -p yes

To build using Golang 1.13 or later, just run `make`

Install this for unit tests:

`go install gotest.tools/gotestsum@latest`

To run unit tests:

`make small-tests`
