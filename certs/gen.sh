#!/bin/bash -xe

openssl req -new -x509 -days 365 -keyout ca.key -out ca.crt -subj "/CN=MyCA"

openssl req -new -newkey rsa:2048 -nodes -keyout server.key -out server.csr -subj "/CN=localhost"
echo "subjectAltName = DNS:localhost" > server.ext
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -extfile server.ext

for name in alice bob; do
	openssl req -new -newkey rsa:2048 -nodes -keyout ${name}.key -out ${name}.csr -subj "/CN=${name}"
	openssl x509 -req -in ${name}.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out ${name}.crt -days 365
done
