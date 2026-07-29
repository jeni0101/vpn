table inet personal_vpn_filter {
    chain input {
        type filter hook input priority filter; policy drop;

        iifname "lo" accept
        ct state invalid drop
        ct state established,related accept
        ip protocol icmp accept
        ip6 nexthdr ipv6-icmp accept
        udp sport 67 udp dport 68 accept
        udp sport 547 udp dport 546 accept

        ip saddr @@MANAGEMENT_IPV4@@ tcp dport @@SSH_PORT@@ accept
        tcp dport { 80, 443 } accept
        udp dport 53147 accept
    }

    chain forward {
        type filter hook forward priority filter; policy accept;

        iifname "wg0" oifname "wg0" drop
        iifname "wg0" oifname "@@WAN_INTERFACE@@" meta nfproto ipv4 accept
        iifname "wg0" oifname "@@WAN_INTERFACE@@" meta nfproto ipv6 drop
        iifname "wg0" drop

        oifname "wg0" ct state established,related accept
        oifname "wg0" drop
    }
}

table ip personal_vpn_nat4 {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        ip saddr 10.68.0.0/24 oifname "@@WAN_INTERFACE@@" masquerade
    }
}
