// Test-only raw ONNX output. Deliberately does not call Chiedi's native wrapper.
#include <onnxruntime_c_api.h>
#include <tokenizers.h>
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

int reference_hidden(const char *library, const char *model, const char *tokenizer_path,
                     const char *text, float **values, size_t *count, char **error) {
 void *handle = NULL, *tokenizer = NULL;
 const OrtApi *api = NULL;
 OrtEnv *env = NULL;
 OrtSessionOptions *options = NULL;
 OrtSession *session = NULL;
 OrtMemoryInfo *memory = NULL;
 OrtTensorTypeAndShapeInfo *shape_info = NULL;
 OrtValue *inputs[3] = {NULL, NULL, NULL}, *output = NULL;
 OrtAllocator *allocator = NULL;
 char *output_name = NULL, *token_error = NULL;
 int64_t *ids = NULL, *mask = NULL, *types = NULL;
 struct tokenizers_buffer encoded = {0};
 int result = 1;
 *values = NULL; *count = 0; *error = NULL;
 #define CHECK(call) do { OrtStatus *s = (call); if (s) { *error = strdup(api->GetErrorMessage(s)); api->ReleaseStatus(s); goto cleanup; } } while(0)
 #define REQUIRE(condition, message) do { if (!(condition)) { *error = strdup(message); goto cleanup; } } while(0)
 handle = dlopen(library, RTLD_NOW | RTLD_LOCAL);
 REQUIRE(handle, "loading reference runtime");
 const OrtApiBase *(*base)(void) = (const OrtApiBase *(*)(void))dlsym(handle, "OrtGetApiBase");
 REQUIRE(base, "finding reference runtime API");
 api = base()->GetApi(ORT_API_VERSION);
 REQUIRE(api, "incompatible reference runtime");
 CHECK(api->CreateEnv(ORT_LOGGING_LEVEL_WARNING, "e5-reference", &env));
 CHECK(api->CreateSessionOptions(&options));
 CHECK(api->SetIntraOpNumThreads(options, 2));
 CHECK(api->SetSessionGraphOptimizationLevel(options, ORT_ENABLE_ALL));
 CHECK(api->CreateSession(env, model, options, &session));
 tokenizer = tokenizers_from_file(tokenizer_path, &token_error);
 REQUIRE(tokenizer, token_error ? token_error : "loading reference tokenizer");
 struct tokenizers_encode_options encode_options = {.add_special_tokens = true};
 encoded = tokenizers_encode(tokenizer, text, &encode_options);
 REQUIRE(encoded.len > 0 && encoded.len <= 512, "reference expects a short input");
 ids = malloc(encoded.len * sizeof(int64_t));
 mask = malloc(encoded.len * sizeof(int64_t));
 types = calloc(encoded.len, sizeof(int64_t));
 REQUIRE(ids && mask && types, "allocating reference input");
 for (size_t i = 0; i < encoded.len; i++) { ids[i] = encoded.ids[i]; mask[i] = 1; }
 CHECK(api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &memory));
 size_t input_count = 0;
 CHECK(api->SessionGetInputCount(session, &input_count));
 REQUIRE(input_count == 2 || input_count == 3, "unexpected reference input count");
 int64_t dims[] = {1, (int64_t)encoded.len};
 int64_t *data[] = {ids, mask, types};
 const char *names[] = {"input_ids", "attention_mask", "token_type_ids"};
 for (size_t i = 0; i < input_count; i++) {
  CHECK(api->CreateTensorWithDataAsOrtValue(memory, data[i], encoded.len * sizeof(int64_t), dims, 2,
        ONNX_TENSOR_ELEMENT_DATA_TYPE_INT64, &inputs[i]));
 }
 CHECK(api->GetAllocatorWithDefaultOptions(&allocator));
 CHECK(api->SessionGetOutputName(session, 0, allocator, &output_name));
 CHECK(api->Run(session, NULL, names, (const OrtValue *const *)inputs, input_count,
       (const char *const *)&output_name, 1, &output));
 CHECK(api->GetTensorTypeAndShape(output, &shape_info));
 size_t rank = 0;
 ONNXTensorElementDataType dtype;
 CHECK(api->GetDimensionsCount(shape_info, &rank));
 CHECK(api->GetTensorElementType(shape_info, &dtype));
 REQUIRE(rank == 3 && dtype == ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, "unexpected reference output type");
 int64_t out_dims[3];
 CHECK(api->GetDimensions(shape_info, out_dims, 3));
 REQUIRE(out_dims[0] == 1 && out_dims[1] == (int64_t)encoded.len && out_dims[2] == 384, "unexpected reference output dimensions");
 float *hidden = NULL;
 CHECK(api->GetTensorMutableData(output, (void **)&hidden));
 *count = encoded.len * 384;
 *values = malloc(*count * sizeof(float));
 REQUIRE(*values, "allocating reference output");
 memcpy(*values, hidden, *count * sizeof(float));
 result = 0;
cleanup:
 if (output_name) allocator->Free(allocator, output_name);
 if (shape_info) api->ReleaseTensorTypeAndShapeInfo(shape_info);
 if (output) api->ReleaseValue(output);
 for (int i = 0; i < 3; i++) if (inputs[i]) api->ReleaseValue(inputs[i]);
 if (memory) api->ReleaseMemoryInfo(memory);
 if (session) api->ReleaseSession(session);
 if (options) api->ReleaseSessionOptions(options);
 if (env) api->ReleaseEnv(env);
 tokenizers_free_buffer(encoded);
 if (tokenizer) tokenizers_free_tokenizer(tokenizer);
 if (token_error) tokenizers_free_string(token_error);
 free(ids); free(mask); free(types);
 if (handle) dlclose(handle);
 return result;
 #undef CHECK
 #undef REQUIRE
}
