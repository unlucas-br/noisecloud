const documentModel = {
    title: "NoiseCloud 2.0 - Relatório Técnico",
    introduction:
        "Este documento resume as diferenças técnicas entre o NoiseCloud Lite original e a linha atual NoiseCloud 2.0. O foco desta versão passa a ser o motor público <strong>Weave 1.0</strong> (<code>WEV1</code>), com pipeline visual mais compacto no modo padrão, reconstrução por blocos com rescue frames e uma experiência de terminal mais limpa para encode e decode.",

    benchmark: {
        title: "1. Benchmark Comparativo: Lite original vs Weave 1.0",
        description:
            "A tabela abaixo preserva as métricas históricas do relatório antigo e adiciona a medição do fluxo público atual com Weave. A coluna antiga foi mantida como referência histórica do NoiseCloud Lite; a coluna Weave 1.0 foi medida localmente no executável público atual com um payload sintético misto de 2.00 MB.",
        metrics: [
            {
                label: "Tamanho do arquivo original",
                legacy: "2.00 MB",
                weave: "2.00 MB (2,097,152 bytes)",
                obs: "Base historica mantida no relatorio antigo e amostra equivalente usada no benchmark Weave atual."
            },
            {
                label: "Tamanho pós-compressão Gzip",
                legacy: "1.00 MB (1,051,187 bytes)",
                weave: "1.00 MB (1,052,730 bytes)",
                obs: "Os dois cenários mantêm redução próxima de 50%, confirmando que o gargalo principal continua sendo o transporte visual."
            },
            {
                label: "Quadros ocupados",
                legacy: "3865 frames",
                weave: "2905 frames",
                obs: "O preset Weave atual reduziu a ocupação em cerca de 24.8% e gerou 96.83 s de vídeo a 30 fps."
            },
            {
                label: "Tempo total de encode",
                legacy: "26.87 segundos",
                weave: "7.40 segundos",
                obs: "No teste local do Weave público, o encode caiu cerca de 72.5% em relação ao benchmark histórico do Lite."
            },
            {
                label: "Tamanho final do vídeo MP4",
                legacy: "14.36 MB",
                weave: "3.15 MB (3,297,777 bytes)",
                obs: "O mp4 do Weave atual ficou cerca de 78.1% menor do que o resultado histórico do preset antigo."
            },
            {
                label: "Tempo total de decode",
                legacy: "46.32 segundos",
                weave: "7.51 segundos",
                obs: "No snapshot público atual, o decode local caiu cerca de 83.8% em comparação com a medição histórica."
            },
            {
                label: "Integridade da reconstrução",
                legacy: "100% validada",
                weave: "100% validada",
                obs: "O benchmark Weave atual fechou com SHA-256 idêntico entre o arquivo original e o recuperado."
            }
        ]
    },

    sections: [
        {
            title: "2. O Que Mudou na Arquitetura",
            intro: "A release 2.0 reorganiza a narrativa técnica do projeto: o modo padrão agora é descrito e entregue como um codec visual próprio, em vez de um encode público centrado em Reed-Solomon por frame.",
            reference: { file: "internal/weave/doc.go", func: "documentação do motor WEV1" },
            content:
                "O novo núcleo <strong>Weave</strong> trata o payload como um fluxo organizado em blocos, com estrutura própria para cabeçalho, dados e rescue frames. Isso deixa mais claro onde está a resiliência do sistema e separa melhor o transporte visual da lógica de recuperação."
        },
        {
            title: "3. Diferenças Centrais entre Lite e 2.0",
            intro: "As mudanças mais importantes desta release aparecem no caminho padrão de encode/decode, no preset visual e na experiência do terminal.",
            subsections: [
                {
                    title: "3.1. Motor Público Padrão: Weave em vez de Reed-Solomon por frame",
                    reference: { file: "cmd/cli/main.go", func: "opções 1 e 3 usam preset weave" },
                    content:
                        "No projeto original, a documentação e o fluxo público destacavam o uso de Reed-Solomon como motor principal de recuperação visual. Na 2.0, o caminho padrão passa a usar o preset <code>weave</code> e o formato <code>WEV1</code>, que organiza o payload em blocos de frames de dados mais rescue frames.",
                    list: [
                        "O bloco padrão usa <strong>16 data frames + 2 rescue frames</strong>.",
                        "A resiliência deixa de ser apresentada apenas como ECC interno de frame e passa a ser descrita como estratégia de bloco do próprio protocolo visual."
                    ]
                },
                {
                    title: "3.2. Preset Padrão Mais Compacto",
                    reference: { file: "internal/encoder/framer.go", func: "CompactWeaveFrameConfig()" },
                    content:
                        "O modo padrão agora usa um preset visual compacto em <code>640x360</code>, <code>MacroSize 8</code>, <code>30 fps</code> e <code>GrayLevels 2</code>. Essa combinação reduz o custo visual do pipeline principal e deixa o comportamento diário do software mais previsível para encode local.",
                    list: [
                        "O objetivo não é eliminar o overhead do transporte em vídeo, e sim reduzir o custo da versão padrão em comparação com a abordagem anterior mais pesada.",
                        "O modo TikTok continua separado porque foi ajustado para cenários mais hostis de resize e recompressão."
                    ]
                },
                {
                    title: "3.3. Decode com trailer e fallback visual",
                    reference: { file: "internal/decoder/trailer.go", func: "ReadWeaveTrailer()" },
                    content:
                        "A 2.0 adiciona leitura de trailer <code>WEV1</code> antes de cair automaticamente para a reconstrução visual quadro a quadro. Isso cria um caminho mais limpo para recuperação quando o payload anexado ainda está disponível e preserva o fallback tradicional quando o decode precisa voltar para os frames.",
                    list: [
                        "Primeiro tenta trailer.",
                        "Se não existir trailer utilizável, segue para extração de frames e reconstrução visual."
                    ]
                }
            ]
        },
        {
            title: "4. Encode na 2.0",
            intro: "O encode continua com compressão Gzip antes da renderização visual, mas a etapa pública foi simplificada ao redor do motor Weave.",
            subsections: [
                {
                    title: "4.1. Compressão antes da malha visual",
                    reference: { file: "cmd/cli/main.go", func: "compressData(data []byte)" },
                    content:
                        "O payload de entrada continua passando por Gzip em memória antes de ser convertido em vídeo. Isso permanece essencial porque a capacidade visual por frame ainda é o gargalo natural do sistema.",
                    list: []
                },
                {
                    title: "4.2. Renderização visual do preset weave",
                    reference: { file: "internal/encoder/video.go", func: "EncodePayloadsWeave()" },
                    content:
                        "No modo padrão, a renderização H.264 trabalha com o preset <code>weave</code> e perfil público mais compacto. O encode usa <code>faststart</code> e parâmetros de saída pensados para manter robustez visual sem voltar ao custo do preset HD anterior.",
                    list: [
                        "Preset padrão: <code>slow + CRF 32</code>.",
                        "Preset TikTok: <code>slow + CRF 15</code>."
                    ]
                }
            ]
        },
        {
            title: "5. Decode na 2.0",
            intro: "A lógica de decode continua dependente da extração limpa de frames e da calibração visual, mas o fluxo foi reorganizado para encaixar o protocolo Weave.",
            subsections: [
                {
                    title: "5.1. Extração limpa de frames",
                    reference: { file: "internal/decoder/extractor.go", func: "ExtractFrames()" },
                    content:
                        "O FFmpeg continua sendo usado para extrair snapshots estáticos em diretoria temporária, mantendo a análise visual separada do playback do vídeo.",
                    list: []
                },
                {
                    title: "5.2. Calibração, alinhamento e reconstrução",
                    reference: { file: "internal/decoder/reconstructor.go", func: "ReconstructWeaveFile()" },
                    content:
                        "Depois da extração, o decoder continua usando barra de calibração, leitura por macropixels e tentativas de alinhamento para recuperar o payload. A diferença agora é que a reconstituição final se ancora no formato <code>WEV1</code> e em sua estratégia de rescue por bloco.",
                    list: [
                        "A calibração ainda é decisiva para lidar com perdas do H.264.",
                        "O fallback visual segue sendo importante para vídeos baixados de plataformas."
                    ]
                }
            ]
        },
        {
            title: "6. Melhorias de UX e Operação",
            intro: "Uma parte importante da 2.0 não está apenas no codec, mas no uso diário do programa.",
            reference: { file: "cmd/cli/main.go", func: "menu e mensagens de operação" },
            content:
                "O terminal foi revisado para reduzir ruído e tornar o fluxo de encode/decode mais claro. O banner verboso do FFmpeg foi escondido no caminho principal, as mensagens ficaram consistentes entre as quatro operações e o menu passou a refletir melhor o estado do processo.",
            highlight: {
                title: "Impacto prático:",
                text: "A melhora de UX não acelera por si só o tempo bruto do encode ou do decode, mas reduz atrito operacional, deixa os estados do processo mais claros e aproxima a versão publicada do comportamento esperado por quem usa a ferramenta no dia a dia."
            }
        }
    ],

    conclusion:
        "A versão 2.0 marca a transição do NoiseCloud Lite para um fluxo público baseado em Weave. O projeto fica mais coerente tecnicamente, ganha um preset padrão mais compacto, melhora a experiência de terminal e passa a documentar explicitamente o protocolo WEV1 como núcleo do caminho principal de encode e decode."
};

class DocumentController {
    constructor(model, containerId) {
        this.model = model;
        this.container = document.getElementById(containerId);
    }

    render() {
        let html = `<h1>${this.model.title}</h1>`;
        html += `<p>${this.model.introduction}</p>`;

        html += `<h2>${this.model.benchmark.title}</h2>`;
        html += `<p>${this.model.benchmark.description}</p>`;
        html += `<table><thead><tr><th>Métrica</th><th>Lite original</th><th>Weave 1.0</th><th>Observação</th></tr></thead><tbody>`;

        this.model.benchmark.metrics.forEach(m => {
            html += `<tr>
                        <td>${m.label}</td>
                        <td>${m.legacy}</td>
                        <td>${m.weave}</td>
                        <td>${m.obs}</td>
                     </tr>`;
        });
        html += `</tbody></table>`;

        this.model.sections.forEach(section => {
            html += `<h2>${section.title}</h2>`;
            if (section.intro) html += `<p>${section.intro}</p>`;

            if (section.content && !section.subsections) {
                if (section.reference) {
                    html += `<div class="reference">Arquivo: <code>${section.reference.file}</code>`;
                    if (section.reference.func) html += `<br>Função: <code>${section.reference.func}</code>`;
                    html += `</div>`;
                }
                html += `<p>${section.content}</p>`;
                if (section.highlight) {
                    html += `<div class="highlight-box">
                                <div class="highlight-title">${section.highlight.title}</div>
                                <p>${section.highlight.text}</p>
                             </div>`;
                }
            }

            if (section.subsections) {
                section.subsections.forEach(sub => {
                    html += `<h3>${sub.title}</h3>`;

                    if (sub.reference) {
                        html += `<div class="reference">Arquivo: <code>${sub.reference.file}</code>`;
                        if (sub.reference.func) html += `<br>Configuração/Função: <code>${sub.reference.func}</code>`;
                        html += `</div>`;
                    }

                    html += `<p>${sub.content}</p>`;

                    if (sub.list && sub.list.length > 0) {
                        html += `<ul>`;
                        sub.list.forEach(li => {
                            html += `<li>${li}</li>`;
                        });
                        html += `</ul>`;
                    }
                });
            }
        });

        html += `<hr style="margin-top: 40px; border: 0; border-top: 1px solid var(--border-color);">`;
        html += `<p style="text-align: center; color: #6b7280; font-size: 0.9em;"><strong>Conclusão técnica:</strong> ${this.model.conclusion}</p>`;

        this.container.innerHTML = html;
    }
}

window.onload = () => {
    const app = new DocumentController(documentModel, "app-view");
    app.render();
};

window.showSection = function (section) {
    const articleView = document.getElementById("article-view");
    const appView = document.getElementById("app-view");
    const btnArticle = document.getElementById("btn-article");
    const btnReport = document.getElementById("btn-report");

    if (section === "article") {
        articleView.style.display = "block";
        appView.style.display = "none";
        btnArticle.classList.add("active");
        btnReport.classList.remove("active");
    } else {
        articleView.style.display = "none";
        appView.style.display = "block";
        btnArticle.classList.remove("active");
        btnReport.classList.add("active");
    }
};
