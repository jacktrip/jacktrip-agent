# JackTrip Virtual Studio Agent

This repository includes the golang source code used to build the
jacktrip-agent executable. This agent is used on both Virtual Studio devices
and servers. It communicates with the back-end REST APIs to retrieve
configuration, and interacts with systemd to manage various system processes.

To build using Golang 1.13 or later, just run `make`
