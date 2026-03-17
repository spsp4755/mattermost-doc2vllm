# Local Testing Setup

This workspace is prepared to test the Mattermost Doc2VLLM plugin locally on Windows.

## Installed Toolchains

- Go: `C:\Users\USER\Documents\Playground\tools\go`
- Node.js/npm: `C:\Users\USER\Documents\Playground\tools\node`
- Python (general): `C:\Users\USER\Documents\Playground\tools\python313`
- Python (HunyuanOCR-pinned): `C:\Users\USER\Documents\Playground\tools\python313-hunyuan`

## Plugin Checks

Server tests:

```powershell
$env:GOCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-build'
$env:GOMODCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-mod'
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' test ./server/...
```

Webapp type check:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run check-types
```

Webapp tests:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' test -- --runInBand
```

Webapp build:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run build
```

Server build:

```powershell
$env:GOCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-build'
$env:GOMODCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-mod'
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' run ./build/manifest apply
Push-Location server
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' build -trimpath -o dist\plugin-windows-amd64.exe
Pop-Location
```

## OCR Model Smoke Tests

The smoke test creates a synthetic image and runs OCR locally against the GPU.

Run all three:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run_ocr_smoke_tests.ps1
```

This wrapper uses `--local-files-only`, so after the first successful downloads it can rerun fully from cache.

Run one model:

```powershell
$env:HF_HOME='C:\Users\USER\Documents\Playground\repo\.hf-cache'
& 'C:\Users\USER\Documents\Playground\tools\python313\python.exe' .\scripts\ocr_model_smoke_test.py --model glm-ocr
& 'C:\Users\USER\Documents\Playground\tools\python313\python.exe' .\scripts\ocr_model_smoke_test.py --model paddleocr-vl-1.5
& 'C:\Users\USER\Documents\Playground\tools\python313-hunyuan\Scripts\python.exe' .\scripts\ocr_model_smoke_test.py --model hunyuanocr
```

Korean smoke test example:

```powershell
$env:HF_HOME='C:\Users\USER\Documents\Playground\repo\.hf-cache'
& 'C:\Users\USER\Documents\Playground\tools\python313\python.exe' .\scripts\ocr_model_smoke_test.py --model glm-ocr --locale ko
```

Offline rerun from cache:

```powershell
$env:HF_HOME='C:\Users\USER\Documents\Playground\repo\.hf-cache'
& 'C:\Users\USER\Documents\Playground\tools\python313\python.exe' .\scripts\ocr_model_smoke_test.py --model glm-ocr --local-files-only
& 'C:\Users\USER\Documents\Playground\tools\python313\python.exe' .\scripts\ocr_model_smoke_test.py --model paddleocr-vl-1.5 --local-files-only
& 'C:\Users\USER\Documents\Playground\tools\python313-hunyuan\Scripts\python.exe' .\scripts\ocr_model_smoke_test.py --model hunyuanocr --local-files-only
```

Outputs are written to `artifacts/ocr-smoke/*.json`.

## Notes

- `GLM-OCR` and `PaddleOCR-VL-1.5` worked in the main Python environment with the latest `transformers` main branch.
- `HunyuanOCR` required a separate Python environment pinned to the model card's recommended `transformers` commit.
- Model downloads are cached under `.hf-cache`.
