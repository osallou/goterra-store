# Goterra

## Status

In development

## About

goterra is a server and a client to exchange data in a terraform deployment.

In fact, it can be used in any kind of deployment where some components need to exchange some data for setup, but it was created in a terraform focus.

For example, we want to create a cluster with 1 master and 2 slaves.
At first we deploy the master which generates a cluster token.
Slaves need this token at install to join the master.

* we install master, master generates token
* goterra client sends token value to goterra server
* we deploy slaves, slaves query goterra server to get token value

The client *get* option tries to fetch a value from server, and waits until value is available or timeout is reached.

## Requirements

Needs Redis

## Config

See goterra.yml.example

## How-to

goterra server has an API to:

* create a deployment
* get a value
* set a value
* delete a deployment

Basically, the steps are:

* create a deployment from host managing the installs, it returns a server address and a token
* during install scripts, install the goterra client and get or set values (my_master_token=XXX, my_ip=YYY, ...)
* after install, delete deployment (clear data in database)

Server can be queried during deployment and until it is deleted via API/CLI

When querying a deployment (ID=XX), one must fill the Authorization HTTP header with the token returned at creation.
This token can be used only for the created deployment (ID=XX) and not for other created deployments. Token expires in 24h.

## Client

See goterra-cli usage

    goterra-cli -h

## Server

Expect goterra.yml in same directory

    goterra

## TODO

Add API to renew token before expiration
