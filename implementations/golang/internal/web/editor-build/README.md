# CodeMirror asset build

This directory produces the locally served CodeMirror 6 editor used for item
detail Markdown. The generated `../static/codemirror.min.js` file is checked in;
ordinary Go builds do not require Node or npm.

To update it, change only exact dependency versions in `package.json`, then run:

```sh
npm install
npm run build
shasum -a 256 ../static/codemirror.min.js
```

Update `../static/codemirror.vendor.txt` with the versions, checksum, and measured
sizes. Package license texts are collected in `../static/codemirror.LICENSE.txt`.

The bundle deliberately omits Vim bindings. T-0276 owns that optional extension.
