import argparse
import json
import os
from pathlib import Path

import torch
from PIL import Image, ImageDraw, ImageFont
from transformers import (
    AutoConfig,
    AutoModelForImageTextToText,
    AutoProcessor,
)


ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS_DIR = ROOT / "artifacts" / "ocr-smoke"

MODEL_CONFIG = {
    "glm-ocr": {
        "repo": "zai-org/GLM-OCR",
        "prompt": "Text Recognition:",
        "family": "auto_image_text_to_text",
    },
    "paddleocr-vl-1.5": {
        "repo": "PaddlePaddle/PaddleOCR-VL-1.5",
        "prompt": "OCR:",
        "family": "auto_image_text_to_text",
    },
    "hunyuanocr": {
        "repo": "tencent/HunyuanOCR",
        "prompt": "Please extract all visible text from the image.",
        "family": "hunyuan",
    },
}


def load_font(size: int):
    candidates = [
        "C:/Windows/Fonts/malgun.ttf",
        "C:/Windows/Fonts/malgunbd.ttf",
        "C:/Windows/Fonts/arial.ttf",
    ]
    for candidate in candidates:
        try:
            return ImageFont.truetype(candidate, size)
        except OSError:
            continue
    return ImageFont.load_default()


def ensure_sample_image(locale: str) -> Path:
    ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)
    sample_image_path = ARTIFACTS_DIR / f"sample-{locale}.png"
    if sample_image_path.exists():
        return sample_image_path

    image = Image.new("RGB", (1600, 900), "white")
    draw = ImageDraw.Draw(image)
    font = load_font(52)
    title_font = load_font(68)

    if locale == "ko":
        lines = [
            ("매터모스트 OCR 스모크 테스트", title_font),
            ("문서 번호: 문서-2026-0317", font),
            ("고객명: 오픈AI 코리아", font),
            ("합계 금액: 12,345원", font),
            ("질문 대상: 문서 번호가 무엇인가요?", font),
        ]
    else:
        lines = [
            ("Mattermost OCR Smoke Test", title_font),
            ("Invoice No: INV-2026-0317", font),
            ("Customer: OpenAI Korea", font),
            ("Total Amount: $12,345.67", font),
            ("Question target: What is the invoice number?", font),
        ]

    y = 120
    for text, current_font in lines:
        draw.text((100, y), text, fill="black", font=current_font)
        y += 120

    image.save(sample_image_path)
    return sample_image_path


def resolve_device():
    if torch.cuda.is_available():
        return "cuda", torch.bfloat16
    return "cpu", torch.float32


def run_auto_image_text_to_text(model_name: str, image_path: Path, local_files_only: bool):
    config = MODEL_CONFIG[model_name]
    device, dtype = resolve_device()
    processor = AutoProcessor.from_pretrained(
        config["repo"],
        trust_remote_code=True,
        local_files_only=local_files_only,
    )
    model_config = AutoConfig.from_pretrained(
        config["repo"],
        trust_remote_code=True,
        local_files_only=local_files_only,
    )
    if model_name == "paddleocr-vl-1.5" and not hasattr(model_config, "text_config") and hasattr(model_config, "get_text_config"):
        model_config.text_config = model_config.get_text_config()
    model = AutoModelForImageTextToText.from_pretrained(
        config["repo"],
        config=model_config,
        dtype=dtype,
        trust_remote_code=True,
        device_map="auto" if device == "cuda" else None,
        local_files_only=local_files_only,
    )
    if device != "cuda":
        model = model.to(device)
    model.eval()

    messages = [
        {
            "role": "user",
            "content": [
                {"type": "image", "image": Image.open(image_path).convert("RGB")},
                {"type": "text", "text": config["prompt"]},
            ],
        }
    ]

    inputs = processor.apply_chat_template(
        messages,
        add_generation_prompt=True,
        tokenize=True,
        return_dict=True,
        return_tensors="pt",
    )
    inputs = {key: value.to(model.device) if hasattr(value, "to") else value for key, value in inputs.items()}
    inputs.pop("token_type_ids", None)

    with torch.no_grad():
        generated_ids = model.generate(**inputs, max_new_tokens=512, do_sample=False)

    prompt_length = inputs["input_ids"].shape[1]
    text = processor.decode(generated_ids[0][prompt_length:], skip_special_tokens=True)
    return text.strip()


def run_hunyuan(model_name: str, image_path: Path, local_files_only: bool):
    from transformers import AutoProcessor, HunYuanVLForConditionalGeneration

    config = MODEL_CONFIG[model_name]
    device, dtype = resolve_device()
    processor = AutoProcessor.from_pretrained(
        config["repo"],
        use_fast=False,
        trust_remote_code=True,
        local_files_only=local_files_only,
    )
    model = HunYuanVLForConditionalGeneration.from_pretrained(
        config["repo"],
        dtype=dtype,
        attn_implementation="eager",
        trust_remote_code=True,
        device_map="auto" if device == "cuda" else None,
        local_files_only=local_files_only,
    )
    if device != "cuda":
        model = model.to(device)
    model.eval()

    messages = [
        {"role": "system", "content": ""},
        {
            "role": "user",
            "content": [
                {"type": "image", "image": str(image_path)},
                {"type": "text", "text": config["prompt"]},
            ],
        },
    ]
    prompt = processor.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
    image = Image.open(image_path).convert("RGB")
    inputs = processor(text=[prompt], images=image, padding=True, return_tensors="pt")
    inputs = inputs.to(model.device)

    with torch.no_grad():
        generated_ids = model.generate(**inputs, max_new_tokens=512, do_sample=False)

    input_ids = inputs["input_ids"]
    generated_ids_trimmed = [out_ids[len(in_ids):] for in_ids, out_ids in zip(input_ids, generated_ids)]
    outputs = processor.batch_decode(
        generated_ids_trimmed,
        skip_special_tokens=True,
        clean_up_tokenization_spaces=False,
    )
    return outputs[0].strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", choices=sorted(MODEL_CONFIG.keys()), required=True)
    parser.add_argument("--output", default=str(ARTIFACTS_DIR / "result.json"))
    parser.add_argument("--local-files-only", action="store_true")
    parser.add_argument("--locale", choices=["en", "ko"], default="en")
    args = parser.parse_args()

    image_path = ensure_sample_image(args.locale)
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)

    if MODEL_CONFIG[args.model]["family"] == "hunyuan":
        result = run_hunyuan(args.model, image_path, args.local_files_only)
    else:
        result = run_auto_image_text_to_text(args.model, image_path, args.local_files_only)

    payload = {
        "model": args.model,
        "repo": MODEL_CONFIG[args.model]["repo"],
        "image": str(image_path),
        "output": result,
    }
    output_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(payload, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    os.environ.setdefault("HF_HOME", str(ROOT / ".hf-cache"))
    os.environ.setdefault("TRANSFORMERS_CACHE", str(ROOT / ".hf-cache" / "transformers"))
    main()
