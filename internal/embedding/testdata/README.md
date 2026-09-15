# Independent E5 reference

`e5-reference.json` contains five multilingual query/passage vectors generated
with the previous independent reference script (`scripts/e5-reference.py` at
commit `3e54e7f7d6b64c772c2660f75d56b8cf794a1680`). It uses NumPy 2.2.6,
ONNX Runtime 1.23.2 (CPU, two intra-op threads), and Hugging Face tokenizers
0.22.2 with the model and prepared tokenizer pinned in `scripts/native-assets.json`.

Each input is prefixed with `query: ` or `passage: `, encoded with special
tokens, then passed to ONNX with an all-ones attention mask and zero token
types. The reference averages the output token vectors in float64 and applies
L2 normalization. The Go test compares each coordinate with tolerance 2e-4.

The fixture was retained from the independently generated reference, not
regenerated with the Go implementation under test. Setup and tests do not need
Python. If the model, tokenizer, or pooling contract changes, regenerate and
review this fixture using an independent implementation; do not replace it
with output from Chiedi itself. `CHIEDI_E5_REFERENCE` can override the fixture
path when checking a freshly generated reference.
