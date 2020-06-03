# High level

The primary objective of the project develops a distributed system and API abstraction to support 
TCP/UDP flow synchronization between different data centers and cluster members, 
the status of the application.

For example, if we have a set of load balancer that need synchronize that current state.
 
The system supports traditional distribute key-value storage, but the primary motivation provides an 
abstract and API layer that can be easily consumed by a different application 
that requires fast synchronization. 


## Overview. 

* Rocinante's system consists set of controllers node that forms a cluster. A cluster provider the 
capability to store key-value a data, and a REST interface (server and client) client 
that can serialize data , via REST API interface.

* In order cluster maintain synchronization and consensus cluster implements RAFT protocol. 

* The communication between cluster member done via gRPC interface.

* The initial leader election protocol follow RAFT specification.  


## Initial protocol.

The implementation follow RAFT recommendation and split consensus model to set of sub
problems.

* Leader Election
* Log Replication

During a start up phase a system reads a configuration specification.  The server specification 
consists a spec for each node in the cluster. For example spec consists of IP and port number pair 
for each node in the cluster, the REST api interface , that also shared with a build-in 
web server, that provide simple UI dashboard interface.

A Rocinante will automatically allocate internal server id for each member node.  The current scheme 
uses hash(IP:PORT) and generates a 64-bit identifier for each node.

In current implementation the cluster communication between each member done via gRPC transport 
protocol, and it supports generic binding via protobuf

* Below example of configuration if we want run 3 instances on localhost.

```
artifact:
  cleanupOnFailure: true
  cluster:
    name: test
    controllers:
      - address: 127.0.0.1
        port: 35001
        rest: 8001
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
      - address: 127.0.0.1
        port: 35002
        rest: 8002
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
      - address: 127.0.0.1
        port: 35003
        rest: 8003
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
```

After reading a configuration and deciding what port to listen, For example, if we want to run a Rocinante 
on the same server for debug purpose. Rocinante will automatically check port allocation and bind each server 
instance to a respected TCP/UDP port.

After server finish initial configuration it will move itself to Follower state based on protocol specification.
If cluster already stable state than a node will remain in same Follower state and will recieve messages 
from a leader.

RAFT specification indicates that at any given time, each server must be in one of three states: leader, follower, 
or candidate. Rocinante adds additional state to a protocol. The primary purpose signal to a network that server 
is not ready,  the motivation behind that two use cases when we want gracefully shut down a server or 
shutdown a GRPC or any other external interfaces.

In the second use case,  if the server needs to perform initial IO operation that require a significant amount of time,  
for example during initial boot - start up time, it can't accept can't accept communication.

Rocinante supports variable timers and generally observation with the semantics described in the original  paper, 
related explicitly to heartbeat timer. If a network provides stable connectivity, then the same node will remain 
a leader for a very long time, since there is no need to re-elect a leader during a stable state.

Rocinante adds added additional options to randomly and none deterministically change a leader by delaying heartbeat 
messages for sufficiently long time so other node force re-elect a new leader. 

As I've mentioned, each server communicates using remote procedure calls and implement raft spec that 
defines two types of RPCs. RequestVote RPCs are initiated by candidates during elections  and Append-Entries 
RPCs are initiated by leaders to replicate log entries and to provide a form of a heartbeat.

Rocinante gRPC interface uses none blocking semantics to communicate between each node in a cluster, and between client 
servers,  rocinante also leverage separate goroutine and for heartbeat channel, communication to external cliennts,
internal communication for commit channels.

In the current list of todo, I have the plan to add an ingress buffer channel to absorb a small amount of gRPC messages.    
Essentially, during transmit and receive routine, server doesn't hold the lock to process RPC message as quickly as 
it can, but as soon as the server start processing, it must hold a lock.  So one idea creates a buffered 
channel to sink RPC message.  


## Storage
 
At current state, system support in-memory storage that provides fast O(1) access to key value pair 
or persistent storage interface.  The current semantics doesn't use an optimized IO layer and 
leverage go gob library to serialize data to persistent storage.
