[Interface]
PrivateKey = @@CLIENT_PRIVATE_KEY@@
Address = @@CLIENT_IPV4@@/32, @@CLIENT_IPV6@@/128
DNS = @@VPN_DNS@@
MTU = @@VPN_MTU@@

[Peer]
PublicKey = @@SERVER_PUBLIC_KEY@@
PresharedKey = @@PRESHARED_KEY@@
Endpoint = @@VPN_ENDPOINT_IPV4@@:@@VPN_PORT@@
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = @@VPN_KEEPALIVE@@
