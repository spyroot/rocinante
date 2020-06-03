# High level

The primary objective of the project develops a distributed system and API abstraction to support 
TCP/UDP flow synchronization between different data centers and cluster members, 
the status of the application.

For example, if we have a set of load balancers that need synchronize that current state.
 
The system supports traditional distribute key-value storage, but the primary motivation provides an 
abstract and API layer that can be easily consumed by a different application 
that requires fast synchronization. 


Overview. 

* Rocinante system consists set of controllers node that forms a cluster. A cluster provider the 
capability to store key-value a data, and a REST client that can serialize data , via REST API interface.

* In order cluster maintain synchronization and consensus cluster implements RAFT protocol. 

* The communication between cluster member done via gRPC interface.
