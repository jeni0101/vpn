table inet personal_vpn_filter {
    chain input {
        type filter hook input priority filter; policy drop;

        iifname "lo" accept
        ct state invalid drop
        ct state established,related accept

        ip protocol icmp accept
        ip6 nexthdr ipv6-icmp accept

        # Preserve cloud DHCP renewals.
        udp sport 67 udp dport 68 accept
        udp sport 547 udp dport 546 accept

        # SSH and website ports. The VPN binds only its high UDP port.
        tcp dport { @@SSH_PORT@@, 80, 443 } accept
        udp dport { 443, @@VPN_PORT@@ } accept
    }

    chain forward {
        type filter hook forward priority filter; policy accept;

        # VPN peers may not initiate traffic to one another.
        iifname "@@VPN_INTERFACE@@" oifname "@@VPN_INTERFACE@@" drop

        # Permit tunnel clients to use the server as their Internet gateway.
        iifname "@@VPN_INTERFACE@@" oifname "@@WAN_INTERFACE@@" accept
        iifname "@@VPN_INTERFACE@@" drop

        # Only established return traffic may enter a VPN peer.
        oifname "@@VPN_INTERFACE@@" ct state established,related accept
        oifname "@@VPN_INTERFACE@@" drop
    }
}

table ip personal_vpn_nat4 {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        ip saddr @@VPN_IPV4_NETWORK@@ oifname "@@WAN_INTERFACE@@" masquerade
    }
}

table ip6 personal_vpn_nat6 {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        ip6 saddr @@VPN_IPV6_NETWORK@@ oifname "@@WAN_INTERFACE@@" masquerade
    }
}
