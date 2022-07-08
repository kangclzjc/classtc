# classtc
Use go tc to configure limit

## NIC discover
IP autodetection methods
When Calico is used for routing, each node must be configured with an IPv4 address and/or an IPv6 address that will be used to route between nodes. To eliminate node specific IP address configuration, the calico/node container can be configured to autodetect these IP addresses. In many systems, there might be multiple physical interfaces on a host, or possibly multiple IP addresses configured on a physical interface. In these cases, there are multiple addresses to choose from and so autodetection of the correct address can be tricky.

The IP autodetection methods are provided to improve the selection of the correct address, by limiting the selection based on suitable criteria for your deployment.

The following sections describe the available IP autodetection methods.

first-found
The first-found option enumerates all interface IP addresses and returns the first valid IP address (based on IP version and type of address) on the first valid interface. Certain known “local” interfaces are omitted, such as the docker bridge. The order that both the interfaces and the IP addresses are listed is system dependent.

This is the default detection method. However, since this method only makes a very simplified guess, it is recommended to either configure the node with a specific IP address, or to use one of the other detection methods.