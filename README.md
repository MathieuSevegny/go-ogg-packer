# go-ogg-packer
**!Library is incomplete, work in progress.**

Pack your opus, alaw, linear16 data into ogg container using Go

Project plan:
- [x] Use AudioBufferWriter wrapper for CGo ogg_packer to get green tests before implementing native Go code
- [x] Dirty native Go ogg packer implementation + comparison auto tests
- [x] Add better error handling, split `packer` package to several files/packages
- [ ] Add linters and check lib structure with peers
- [ ] Production-ready release 1.0
