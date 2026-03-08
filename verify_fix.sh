#!/bin/bash
echo '{"image": "ubuntu-24.04", "cpu": 1, "ram": 2048, "ssh_key": "ssh-rsa test"}' > test_request.json
curl -X POST http://localhost:8085/api/v1/ec2/instances -H 'Content-Type: application/json' -d @test_request.json

