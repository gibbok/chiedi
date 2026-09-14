// Calls ONNX Runtime's versioned native C API directly, without a service.
#include "native.h"
#include "tokenizers.h"
#include <onnxruntime_c_api.h>
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

struct ChiediEngine {
 void *library;
 const OrtApi *api;
 OrtEnv *env;
 OrtSession *session;
 OrtMemoryInfo *memory;
 void *tokenizer;
 int type_ids;
 char *output;
};

static int failed(ChiediEngine *e, OrtStatus *status, char **error) {
 if (!status) return 0;
 *error = strdup(e->api->GetErrorMessage(status));
 e->api->ReleaseStatus(status);
 return 1;
}

void chiedi_close(ChiediEngine *e) {
 if (!e) return;
 if (e->tokenizer) tokenizers_free_tokenizer(e->tokenizer);
 if (e->memory) e->api->ReleaseMemoryInfo(e->memory);
 if (e->session) e->api->ReleaseSession(e->session);
 if (e->env) e->api->ReleaseEnv(e->env);
 free(e->output);
 if (e->library) dlclose(e->library);
 free(e);
}

ChiediEngine *chiedi_open(const char *library, const char *model, const char *tokenizer, char **error) {
 ChiediEngine *e = calloc(1, sizeof(*e));
 if (!e) { *error = strdup("allocating embedding engine"); return NULL; }
 OrtSessionOptions *options = NULL;
 OrtAllocator *allocator = NULL;
 e->library = dlopen(library, RTLD_NOW | RTLD_LOCAL);
 if (!e->library) { *error = strdup(dlerror()); goto fail; }
 const OrtApiBase *(*base)(void) = (const OrtApiBase *(*)(void))dlsym(e->library, "OrtGetApiBase");
 if (!base) { *error = strdup("missing OrtGetApiBase"); goto fail; }
 e->api = base()->GetApi(ORT_API_VERSION);
 if (!e->api) { *error = strdup("incompatible ONNX Runtime C API"); goto fail; }
 if (failed(e, e->api->CreateEnv(ORT_LOGGING_LEVEL_WARNING, "chiedi", &e->env), error)) goto fail;
 if (failed(e, e->api->CreateSessionOptions(&options), error)) goto fail;
 // A small thread budget avoids oversubscription across concurrent CLI processes.
 if (failed(e, e->api->SetIntraOpNumThreads(options, 2), error)) goto fail;
 if (failed(e, e->api->SetInterOpNumThreads(options, 1), error)) goto fail;
 if (failed(e, e->api->SetSessionGraphOptimizationLevel(options, ORT_ENABLE_ALL), error)) goto fail;
 if (failed(e, e->api->CreateSession(e->env, model, options, &e->session), error)) goto fail;
 e->api->ReleaseSessionOptions(options); options = NULL;
 if (failed(e, e->api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &e->memory), error)) goto fail;
 if (failed(e, e->api->GetAllocatorWithDefaultOptions(&allocator), error)) goto fail;
 size_t count = 0;
 if (failed(e, e->api->SessionGetInputCount(e->session, &count), error)) goto fail;
 int have_ids = 0, have_mask = 0;
 for (size_t i = 0; i < count; i++) {
  char *name = NULL;
  if (failed(e, e->api->SessionGetInputName(e->session, i, allocator, &name), error)) goto fail;
  if (!strcmp(name, "input_ids")) have_ids = 1;
  else if (!strcmp(name, "attention_mask")) have_mask = 1;
  else if (!strcmp(name, "token_type_ids")) e->type_ids = 1;
  else { allocator->Free(allocator,name); *error = strdup("unexpected model input"); goto fail; }
  allocator->Free(allocator,name);
 }
 if (!have_ids || !have_mask || count != (size_t)(2+e->type_ids)) {
  *error = strdup("invalid E5 input signature"); goto fail;
 }
 char *output = NULL;
 if (failed(e, e->api->SessionGetOutputName(e->session, 0, allocator, &output), error)) goto fail;
 e->output = strdup(output); allocator->Free(allocator,output);
 if (!e->output) { *error = strdup("allocating output name"); goto fail; }
 char *token_error = NULL;
 e->tokenizer = tokenizers_from_file(tokenizer, &token_error);
 if (!e->tokenizer) {
  *error = strdup(token_error ? token_error : "loading tokenizer");
  if (token_error) tokenizers_free_string(token_error);
  goto fail;
 }
 return e;
fail:
 if (options) e->api->ReleaseSessionOptions(options);
 chiedi_close(e);
 return NULL;
}

int chiedi_tokens(ChiediEngine *e, const char *text, int64_t **ids, size_t *len, char **error) {
 struct tokenizers_encode_options options = {0};
 struct tokenizers_buffer buffer = tokenizers_encode(e->tokenizer, text, &options);
 *len = buffer.len; *ids = NULL;
 if (buffer.len) {
  *ids = malloc(buffer.len * sizeof(int64_t));
  if (!*ids) { tokenizers_free_buffer(buffer); *error = strdup("allocating token IDs"); return 1; }
  for (size_t i = 0; i < buffer.len; i++) (*ids)[i] = buffer.ids[i];
 }
 tokenizers_free_buffer(buffer);
 return 0;
}

int chiedi_run(ChiediEngine *e, const int64_t *ids, size_t len, float *out, char **error) {
 const char *names[] = {"input_ids", "attention_mask", "token_type_ids"};
 OrtValue *inputs[3] = {NULL,NULL,NULL}, *output = NULL;
 OrtTensorTypeAndShapeInfo *info = NULL;
 int64_t *mask = NULL, *types = NULL;
 int result = 1;
 int64_t shape[] = {1,(int64_t)len};
 if (!len || len > 512) { *error = strdup("invalid E5 token count"); return 1; }
 mask = malloc(len*sizeof(int64_t)); types = calloc(len,sizeof(int64_t));
 if (!mask || !types) { *error = strdup("allocating input tensors"); goto done; }
 for (size_t i = 0; i < len; i++) mask[i] = 1;
 int64_t *data[] = {(int64_t *)ids,mask,types};
 for (int i = 0; i < 2+e->type_ids; i++) {
  if (failed(e,e->api->CreateTensorWithDataAsOrtValue(e->memory,data[i],len*sizeof(int64_t),shape,2,ONNX_TENSOR_ELEMENT_DATA_TYPE_INT64,&inputs[i]),error)) goto done;
 }
 if (failed(e,e->api->Run(e->session,NULL,names,(const OrtValue *const *)inputs,2+e->type_ids,(const char *const *)&e->output,1,&output),error)) goto done;
 if (failed(e,e->api->GetTensorTypeAndShape(output,&info),error)) goto done;
 size_t rank = 0;
 ONNXTensorElementDataType dtype;
 if (failed(e,e->api->GetDimensionsCount(info,&rank),error)) goto done;
 if (failed(e,e->api->GetTensorElementType(info,&dtype),error)) goto done;
 if (rank != 3 || dtype != ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT) { *error = strdup("expected float32 token embeddings"); goto done; }
 int64_t dims[3];
 if (failed(e,e->api->GetDimensions(info,dims,3),error)) goto done;
 if (dims[0] != 1 || dims[1] != (int64_t)len || dims[2] != 384) { *error = strdup("unexpected E5 output shape"); goto done; }
 float *values = NULL;
 if (failed(e,e->api->GetTensorMutableData(output,(void **)&values),error)) goto done;
 // Unpadded windows: attention mask is all ones, including special tokens.
 for (size_t d = 0; d < 384; d++) {
  double sum = 0;
  for (size_t t = 0; t < len; t++) sum += values[t*384+d];
  out[d] = (float)(sum/len);
 }
 result = 0;
done:
 if (info) e->api->ReleaseTensorTypeAndShapeInfo(info);
 if (output) e->api->ReleaseValue(output);
 for (int i = 0; i < 3; i++) if (inputs[i]) e->api->ReleaseValue(inputs[i]);
 free(mask); free(types);
 return result;
}
