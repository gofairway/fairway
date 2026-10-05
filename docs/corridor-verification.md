## NGNC
- Issuer: GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6
- Domain: ngnc.online
- Status: live
- Verified: 3rd October 2026

## GHSC (not usable yet)
- Issuer: GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6
- Domain: ngnc.online
- Status: pending
- Verified: 3rd October 2026

## KESC (not usable yet)
- Issuer: GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6
- Domain: ngnc.online
- Status: pending
- Verified: 3rd October 2026

## Note
A second NGNC asset exists under issuer GBMJQAHHS6...NFWC with
zero funded trustlines — confirmed NOT the real anchor, excluded.

## USDC
- Issuer: GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN
- Domain: centre.io
- Status: no explicit status field in stellar.toml; confirmed live by
  119M+ on-chain payments and attestation_of_reserve link
- Authorization flags: revocable (issuer can claw back/freeze under
  certain conditions)
- Verified: 3rd October 2026

## EURC
- Issuer: GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2
- Domain: none found — no home_domain set, no stellar.toml exists
- Status: UNVERIFIABLE — no SEP-1 metadata despite 525,977 payments,
  $4M+ supply. This is itself a risk signal Fairway should surface.
- Authorization flags: revocable
- Verified: 3rd October 2026

## Known Limitations

- **Fixed Trade Size on Thin Assets**: Single fixed-size (100 unit) measurements on thin assets may not reflect real-world execution at typical remittance volumes.
- **Multi-Hop Path Routing Artifacts**: Multi-hop paths through intermediary assets (such as AQUA, native XLM, or USDC) can show favorable or unfavorable pricing that wouldn't hold at scale due to shallow order-book depth. Large negative loss percentages observed on low-volume corridors (like NGNC pairs) are a known consequence of this routing behavior rather than genuine institutional pricing.