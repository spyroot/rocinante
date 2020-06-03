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

During a start up phase a system read configuration specification.  The specification 
consists of a spec for each node in the cluster.  A system will automatically allocate 
internal server id for each member node.   The current scheme uses hash(IP:PORT) and generates a 64-bit 
identifier for each node.

In current implementation the communication between each member done via gRPC transport 
protocol and it supports generic binding via protobuf

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
 
At this stage, system support in-memory storage that provides fast O(1) access or persistent storage.  
The current semantics doesn't use an optimized IO layer and leverage go gob library to serialize data to persistent storage.
 