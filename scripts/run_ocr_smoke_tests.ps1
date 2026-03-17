$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$MainPython = "C:\Users\USER\Documents\Playground\tools\python313\python.exe"
$HunyuanPython = "C:\Users\USER\Documents\Playground\tools\python313-hunyuan\Scripts\python.exe"
$env:HF_HOME = Join-Path $RepoRoot ".hf-cache"
$env:HF_HUB_DISABLE_SYMLINKS_WARNING = "1"

Push-Location $RepoRoot
try {
    & $MainPython scripts\ocr_model_smoke_test.py --model glm-ocr --local-files-only --output artifacts\ocr-smoke\glm-ocr.json
    & $MainPython scripts\ocr_model_smoke_test.py --model paddleocr-vl-1.5 --local-files-only --output artifacts\ocr-smoke\paddleocr-vl-1.5.json
    & $HunyuanPython scripts\ocr_model_smoke_test.py --model hunyuanocr --local-files-only --output artifacts\ocr-smoke\hunyuanocr.json
} finally {
    Pop-Location
}
