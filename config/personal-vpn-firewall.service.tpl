[Unit]
Description=Personal VPN nftables rules
Wants=network-online.target
After=network-online.target
Before=wg-quick@@VPN_INTERFACE@@.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/personal-vpn-firewall apply
ExecReload=/usr/local/sbin/personal-vpn-firewall apply
ExecStop=/usr/local/sbin/personal-vpn-firewall remove

[Install]
WantedBy=multi-user.target
