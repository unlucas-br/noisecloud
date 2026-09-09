# Atribuições

## CHOP

As melhorias do núcleo em `internal/weave` foram adaptadas de
[unlucas-br/CHOP](https://github.com/unlucas-br/CHOP), a partir da integração
`4e34b473a641b2633da483b02e884ff820b62aaa` e das correções presentes no commit
`11c2a771e3203ab7773476546296ff72ac709e16`.

Copyright (c) 2026 Lucas Ferraz. Licença BSD-3-Clause, reproduzida em
[`licenses/chop.txt`](licenses/chop.txt). O NoiseCloud mantém sua licença GPL-3.0.

Os limites foram adaptados ao transporte de arquivos completos por vídeo.
O formato de armazenamento CHOP, seu modo sem paridade e seu preditor PCM
não são usados no transporte de vídeo do NoiseCloud.

## Zstandard

`github.com/klauspost/compress` fornece a implementação Zstandard.
Sua licença está reproduzida em [`licenses/klauspost-compress.txt`](licenses/klauspost-compress.txt).

## FFmpeg integrado nas releases Windows

O executável inclui o FFmpeg 9.0.1, build estático `essentials` de
[Gyan Doshi](https://www.gyan.dev/ffmpeg/builds/), sob GPLv3. O arquivo original
e o executável são verificados por SHA-256 antes de serem incorporados; os
valores e a URL imutável estão em [`internal/ffmpeg/bundle.json`](internal/ffmpeg/bundle.json).

O build original, suas bibliotecas e sua configuração estão descritos no
`README.txt` distribuído por Gyan. Esse arquivo e a licença original são
incluídos no EXE e extraídos ao lado do runtime; também acompanham o ZIP em
`licenses/ffmpeg-build.txt` e `licenses/ffmpeg.txt`.

Fonte do FFmpeg deste build: [FFmpeg/FFmpeg, bf1b838f2a](https://github.com/FFmpeg/FFmpeg/commit/bf1b838f2a).
Distribuição original: [GyanD/codexffmpeg 9.0.1](https://github.com/GyanD/codexffmpeg/releases/tag/9.0.1).
