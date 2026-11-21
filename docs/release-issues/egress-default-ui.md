# Admin UI: present default and named SOCKS5 exits as peers

**Severity:** low (P3), deferred UX; preserve beta.4.3 compatibility. **Component:** Admin/subscriptions.

The first exit is labelled default/primary for historical clients, while beta.4.4 can select additional named exits via authenticated `egress_id`. Both provide the same routing capability. Show one uniform list of exits with a visible **Default for legacy clients** property; do not imply one country is faster, more secure or prioritized. Keep existing stable profile IDs, canonical subscription URLs, v1 response and user secrets. Distinguish **ingress endpoints** (REG.RU/Beget/direct QUIC) from **country exits** (DK/KZ) with clear field labels and ordering. Never create a second ingress just to add a country.
