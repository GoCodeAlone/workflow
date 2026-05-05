# External Plugin Protobufs

Regenerate Go bindings from the repository root:

```sh
buf generate
```

`buf.yaml` and `buf.gen.yaml` pin the input path, generated output paths, and remote generator plugin versions for `plugin.proto`.
