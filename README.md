# McNotify

Use McNotify to send SMS and Email based notifications of individuals
joining your Minecraft server.

Go is currently a requirement to run this package... though it is
also easily built by running `GOOS=linux GOARCH=amd64 go build` to
build this for linux.

To install dependencies, run `go get github.com/untoldone/mcnotify`

To run, copy `run.sample.sh` to `run.sh`, enter in all environment variables
in that file, and then run `./run.sh` from the same director containing `run.sh`.