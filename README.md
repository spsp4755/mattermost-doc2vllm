# Mattermost Doc2VLLM OCR Plugin

This plugin connects Mattermost messages and uploaded image, PDF, DOCX, XLSX, or PPTX files to a vLLM OpenAI-compatible OCR endpoint. Admins can configure multiple Mattermost bot accounts, each with its own OCR model, prompt, output mode, auth settings, and optional vLLM post-processing.

## What It Does

- Receives a Mattermost message plus attached image, PDF, DOCX, XLSX, or PPTX files
- Sends each image page to `chat/completions` as `messages[].content[]` with `text` and `image_url`
- Extracts OCR text from `choices[0].message.content`
- Posts the extracted text back to the channel or thread
- Supports multiple OCR bots with different model and decoding settings

## Main Capabilities

- Multiple Mattermost bot accounts managed by the plugin
- Per-bot OCR options such as `model`, `output_mode`, `ocr_prompt`, `temperature`, `max_tokens`, and `top_p`
- Global service configuration plus per-bot overrides
- Mattermost RHS workflow for choosing a bot and running OCR
- Automatic PDF-to-image conversion before OCR when a rasterizer is installed on the plugin host
- Direct DOCX/XLSX/PPTX text extraction without calling the OCR model
- Optional vLLM post-processing using the OCR result as context
- Access control by user, team, and channel

## Request Shape

The main Doc2VLLM call uses an OpenAI-compatible request like this:

```json
{
  "model": "doc2vllm-ocr",
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "이미지에서 텍스트를 추출해 주세요."
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "data:image/png;base64,..."
          }
        }
      ]
    }
  ],
  "temperature": 0,
  "max_tokens": 1024,
  "top_p": 1
}
```

## Configuration Model

The plugin stores its current configuration in the `Config` JSON field.

```json
{
  "service": {
    "base_url": "http://localhost:8000/v1/chat/completions",
    "auth_mode": "bearer",
    "auth_token": "YOUR_API_KEY",
    "allow_hosts": "localhost"
  },
  "runtime": {
    "default_timeout_seconds": 30,
    "max_input_length": 4000,
    "max_output_length": 8000,
    "pdf_raster_dpi": 200,
    "max_pdf_pages": 20,
    "enable_debug_logs": false,
    "enable_usage_logs": true
  },
  "bots": [
    {
      "username": "doc2vllm-ocr",
      "display_name": "Doc2VLLM OCR",
      "model": "doc2vllm-ocr",
      "output_mode": "markdown",
      "ocr_prompt": "이미지에서 텍스트를 추출해 주세요.",
      "temperature": 0,
      "max_tokens": 1024,
      "top_p": 1
    }
  ]
}
```

## Notes

- Image attachments are sent directly to the OCR model.
- Searchable PDFs use text-layer extraction first when `pdftotext` is available on the plugin host.
- PDF attachments are rasterized to page images first. Install one of `pdftoppm`, `mutool`, `magick`, `gswin64c`, `gswin32c`, or `gs` on the Mattermost plugin host to enable this.
- DOCX attachments are parsed directly from the Office XML package and returned without OCR.
- XLSX attachments are parsed sheet-by-sheet and returned as tab-separated text blocks.
- PPTX attachments are parsed slide-by-slide and returned as slide text blocks.
- `output_mode` can be `markdown`, `text`, or `json` for raw OCR/document extraction responses.
- The plugin limits PDF processing using `pdf_raster_dpi` and `max_pdf_pages` to avoid expensive conversions on very large documents.
- Legacy `.doc`, `.xls`, `.ppt` and other non-OOXML office formats are not directly supported yet.
- If `vllm_base_url` and `vllm_model` are configured on a bot, the plugin can run an extra post-processing step after OCR.

## Development

Server tests:

```bash
go test ./server/...
```

Webapp type check:

```bash
cd webapp
npm run check-types
```

Webapp build:

```bash
cd webapp
npm run build
```
