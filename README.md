# go-ogg-packer
Pack your opus, alaw, linear16 data into ogg container using Go

Project plan:
- [x] Use AudioBufferWriter wrapper for CGo ogg_packer to get green tests before implementing native Go code
- [x] Dirty native Go ogg packer implementation + comparison auto tests
- [x] Add better error handling, split `packer` package to several files/packages
- [x] Check linters
- [x] Remove C ogg lib dependency
- [ ] Remove C opus lib dependency
- [ ] Check lib layout with peers
- [ ] Production-ready release 1.0
