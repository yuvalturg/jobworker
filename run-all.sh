#!/bin/bash -xe

sudo killall jobserver ||:

protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative pkg/api/jobworker.proto

go build -o jobserver cmd/server/main.go
go build -o jobclient cmd/client/main.go

echo "Starting server"
sudo ./jobserver &
sleep 1

echo "--- Running short task ---"
job_id=$(./jobclient start -- ls /dev/null | jq -r '.jobId')
sleep 0.5

stat=$(./jobclient status ${job_id} | jq -r '.status')
if [ ${stat} != "jobStopped" ]; then 
	echo "Expected job status to be jobStopped"
	exit 1
fi

echo "--- Running long task ---"

job_id=$(./jobclient start -- bash -c 'while :; do echo hello; sleep 1; done' | jq -r '.jobId')
sleep 0.5

stat=$(./jobclient status ${job_id} | jq -r '.status')
if [ ${stat} != "jobRunning" ]; then 
	echo "Expected job status to be jobRunning"
	exit 1
fi

# stream in the background
tmpfile=$(mktemp)
./jobclient stream ${job_id} > ${tmpfile} &
streampid=$!

# let it run 3 sec and stop it
sleep 3

./jobclient stop ${job_id}
sleep 0.5

stat=$(./jobclient status ${job_id} | jq -r '.status')
if [ ${stat} != "jobStopped" ]; then 
	echo "Expected job status to be jobStopped"
	exit 1
fi

wait ${streampid}

grep hello ${tmpfile} || {
	echo "Expected hello in output"
	exit 1
}

rm ${tmpfile}
