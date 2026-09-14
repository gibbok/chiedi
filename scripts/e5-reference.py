#!/usr/bin/env python3
"""CI-only independent tokenizer/pooling reference, never shipped or run by Chiedi."""
import json
import os
from pathlib import Path
import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer

assets = Path(os.environ["CHIEDI_ASSETS"])
tokenizer = Tokenizer.from_file(str(assets / "tokenizer.json"))
options = ort.SessionOptions()
options.intra_op_num_threads = 2
session = ort.InferenceSession(str(assets / "model.onnx"), options, providers=["CPUExecutionProvider"])
cases = [
    ("query", "How many vacation days do workers get?"),
    ("query", "Kolik dní dovolené mají zaměstnanci?"),
    ("query", "数据库备份保留多久？"),
    ("passage", "Employees receive twenty days of paid vacation each year."),
    ("passage", "La visita dal dentista è fissata per martedì mattina."),
]
results = []
for kind, text in cases:
    ids = np.array([tokenizer.encode(kind + ": " + text).ids], dtype=np.int64)
    inputs = {"input_ids": ids, "attention_mask": np.ones_like(ids), "token_type_ids": np.zeros_like(ids)}
    inputs = {item.name: inputs[item.name] for item in session.get_inputs()}
    hidden = session.run(None, inputs)[0]
    vector = hidden.astype(np.float64).mean(axis=1)[0]
    vector /= np.linalg.norm(vector)
    results.append({"kind": kind, "text": text, "vector": vector.tolist()})
path = Path(os.environ["CHIEDI_E5_REFERENCE"])
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps(results))
print("Independent ONNX/tokenizer reference written: " + str(path))
