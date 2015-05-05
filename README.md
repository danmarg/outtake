# outtake
Sync Gmail to maildir...quickly!

Unlike offlineimap and similar, *outtake* uses the Gmail API to efficiently sync
only deltas.

Syncing can also be limited to a specific label.

# usage

```
go get github.com/danmarg/outtake
./outtake --directory ~/Mail
```
