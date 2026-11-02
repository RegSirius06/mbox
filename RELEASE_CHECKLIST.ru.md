# Release checklist — русская памятка

[English full checklist](RELEASE_CHECKLIST.md)

Перед тегом `v1.0.0-beta.2.1`:

- чистый git tree и полный test/race/vet/Python gate;
- Android release/debug build на Java 17;
- real-device Cronet+h2 check;
- Wi-Fi/mobile/airplane-mode regression;
- server session supersession check;
- MTS/T2/MegaFon white-list field matrix;
- `.fun`/`.ru` controlled test;
- secret scan;
- docs/version/changelog review;
- никаких production PSK/private keys/DB/provisioning links в git.
