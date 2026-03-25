#!/usr/bin/env python3
"""Generate patent PDF from markdown using fpdf2 with Unicode support."""

import os
import re
from fpdf import FPDF

PATENT_DIR = os.path.dirname(os.path.abspath(__file__))
MD_FILE = os.path.join(PATENT_DIR, "utility_patent_provisional.md")
PDF_FILE = os.path.join(PATENT_DIR, "utility_patent_provisional.pdf")
FIGURES_DIR = os.path.join(PATENT_DIR, "figures")

FONT_DIR = "/System/Library/Fonts/Supplemental"
FONT_REGULAR = os.path.join(FONT_DIR, "Times New Roman.ttf")
FONT_BOLD = os.path.join(FONT_DIR, "Times New Roman Bold.ttf")
FONT_ITALIC = os.path.join(FONT_DIR, "Times New Roman Italic.ttf")
FONT_BOLD_ITALIC = os.path.join(FONT_DIR, "Times New Roman Bold Italic.ttf")


class PatentPDF(FPDF):
    def __init__(self):
        super().__init__()
        self.set_auto_page_break(auto=True, margin=25)
        # Register Unicode TTF fonts
        self.add_font("TNR", "", FONT_REGULAR, uni=True)
        self.add_font("TNR", "B", FONT_BOLD, uni=True)
        self.add_font("TNR", "I", FONT_ITALIC, uni=True)
        self.add_font("TNR", "BI", FONT_BOLD_ITALIC, uni=True)

    def header(self):
        pass

    def footer(self):
        self.set_y(-15)
        self.set_font("TNR", "I", 10)
        self.cell(0, 10, f"Page {self.page_no()}/{{nb}}", align="C")

    def chapter_title(self, title, level=2):
        title = clean_md(title)
        if level == 1:
            self.set_font("TNR", "B", 16)
            self.ln(6)
            self.multi_cell(0, 8, title, align="C")
            self.ln(4)
        elif level == 2:
            self.set_font("TNR", "B", 13)
            self.ln(6)
            self.multi_cell(0, 7, title.upper())
            self.ln(2)
        elif level == 3:
            self.set_font("TNR", "B", 12)
            self.ln(4)
            self.multi_cell(0, 7, title)
            self.ln(2)
        elif level == 4:
            self.set_font("TNR", "BI", 12)
            self.ln(3)
            self.multi_cell(0, 7, title)
            self.ln(2)

    def body_text(self, text):
        self.set_font("TNR", "", 12)
        self.write_formatted(text)
        self.ln(6)

    def write_formatted(self, text):
        """Write text with bold/italic inline formatting."""
        parts = re.split(r'(\*\*.*?\*\*|\*[^*]+?\*)', text)
        for part in parts:
            if part.startswith('**') and part.endswith('**'):
                self.set_font("TNR", "B", 12)
                self.write(6, part[2:-2])
                self.set_font("TNR", "", 12)
            elif part.startswith('*') and part.endswith('*') and not part.startswith('**'):
                self.set_font("TNR", "I", 12)
                self.write(6, part[1:-1])
                self.set_font("TNR", "", 12)
            else:
                self.write(6, part)

    def add_figure(self, img_path, caption=""):
        if os.path.exists(img_path):
            if self.get_y() > 160:
                self.add_page()
            try:
                available_w = self.w - self.l_margin - self.r_margin
                self.image(img_path, x=self.l_margin, w=available_w)
                if caption:
                    self.set_font("TNR", "I", 10)
                    self.ln(2)
                    self.multi_cell(0, 5, caption, align="C")
                self.ln(6)
            except Exception as e:
                self.body_text(f"[Figure: {os.path.basename(img_path)} - {e}]")

    def add_table(self, headers, rows):
        available_w = self.w - self.l_margin - self.r_margin
        col_count = len(headers)
        col_w = available_w / col_count

        # Headers
        self.set_font("TNR", "B", 10)
        self.set_fill_color(230, 230, 230)
        for h in headers:
            self.cell(col_w, 7, clean_md(h.strip()), border=1, fill=True, align="C")
        self.ln()

        # Rows
        self.set_font("TNR", "", 10)
        for row in rows:
            # Pad row to match headers
            while len(row) < col_count:
                row.append("")

            cell_texts = [clean_md(c.strip()) for c in row[:col_count]]

            # Calculate row height
            max_lines = 1
            for ct in cell_texts:
                char_per_line = max(1, int(col_w / 2.2))
                nlines = max(1, (len(ct) // char_per_line) + 1)
                max_lines = max(max_lines, nlines)
            row_h = 6 * max_lines

            if self.get_y() + row_h > self.h - 25:
                self.add_page()
                # Re-draw headers
                self.set_font("TNR", "B", 10)
                self.set_fill_color(230, 230, 230)
                for h in headers:
                    self.cell(col_w, 7, clean_md(h.strip()), border=1, fill=True, align="C")
                self.ln()
                self.set_font("TNR", "", 10)

            y_start = self.get_y()
            x_start = self.l_margin
            max_y = y_start
            for ci, ct in enumerate(cell_texts):
                self.set_xy(x_start + ci * col_w, y_start)
                self.multi_cell(col_w, 6, ct, border=1)
                max_y = max(max_y, self.get_y())

            # Fill any short cells with borders
            for ci, ct in enumerate(cell_texts):
                cell_bottom = y_start
                self.set_xy(x_start + ci * col_w, y_start)
                # The multi_cell already drew borders, just ensure alignment
            self.set_y(max_y)

        self.ln(4)


def clean_md(text):
    """Remove markdown formatting characters."""
    text = re.sub(r'\*\*([^*]+)\*\*', r'\1', text)
    text = re.sub(r'\*([^*]+)\*', r'\1', text)
    text = re.sub(r'`([^`]+)`', r'\1', text)
    return text


def parse_table(lines, start_idx):
    """Parse a markdown table."""
    headers = [c.strip() for c in lines[start_idx].strip().strip('|').split('|')]
    rows = []
    idx = start_idx + 2  # skip separator
    while idx < len(lines) and lines[idx].strip().startswith('|'):
        row = [c.strip() for c in lines[idx].strip().strip('|').split('|')]
        rows.append(row)
        idx += 1
    return headers, rows, idx


def generate_pdf():
    with open(MD_FILE, 'r') as f:
        content = f.read()

    lines = content.split('\n')
    pdf = PatentPDF()
    pdf.alias_nb_pages()
    pdf.add_page()
    pdf.set_margins(25, 25, 25)

    i = 0
    list_buffer = []
    list_ordered = False

    def flush_list():
        nonlocal list_buffer, list_ordered
        if not list_buffer:
            return
        for idx, item in enumerate(list_buffer):
            pdf.set_font("TNR", "", 12)
            text = re.sub(r'^\d+\.\s*', '', item)
            text = re.sub(r'^-\s*', '', text)
            if list_ordered:
                prefix = f"  {idx+1}. "
            else:
                prefix = "  \u2022 "
            pdf.set_x(pdf.l_margin + 5)
            pdf.write_formatted(prefix + text)
            pdf.ln(6)
        list_buffer = []

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()

        # Skip empty lines
        if not stripped:
            flush_list()
            i += 1
            continue

        # Horizontal rule
        if stripped == '---':
            flush_list()
            pdf.ln(2)
            pdf.line(pdf.l_margin, pdf.get_y(), pdf.w - pdf.r_margin, pdf.get_y())
            pdf.ln(4)
            i += 1
            continue

        # Headers
        if stripped.startswith('#'):
            flush_list()
            level = 0
            for ch in stripped:
                if ch == '#':
                    level += 1
                else:
                    break
            title = stripped[level:].strip()
            pdf.chapter_title(title, level)
            i += 1
            continue

        # Images
        img_match = re.match(r'!\[([^\]]*)\]\(([^)]+)\)', stripped)
        if img_match:
            flush_list()
            img_path = img_match.group(2)
            full_path = os.path.join(PATENT_DIR, img_path)
            caption = ""
            if i + 1 < len(lines) and lines[i+1].strip().startswith('*'):
                caption = lines[i+1].strip().strip('*')
                i += 1
            pdf.add_figure(full_path, caption)
            i += 1
            continue

        # Tables
        if stripped.startswith('|') and i + 1 < len(lines) and '---' in lines[i+1]:
            flush_list()
            headers, rows, end_idx = parse_table(lines, i)
            pdf.add_table(headers, rows)
            i = end_idx
            continue

        # Ordered list items
        if re.match(r'^\d+\.', stripped):
            if not list_buffer:
                list_ordered = True
            list_buffer.append(stripped)
            # Gather continuation lines
            while i + 1 < len(lines) and lines[i+1].startswith('   ') and lines[i+1].strip():
                i += 1
                list_buffer[-1] += ' ' + lines[i].strip()
            i += 1
            continue

        # Unordered list items
        if stripped.startswith('- '):
            if not list_buffer:
                list_ordered = False
            list_buffer.append(stripped)
            while i + 1 < len(lines) and lines[i+1].startswith('   ') and lines[i+1].strip():
                i += 1
                list_buffer[-1] += ' ' + lines[i].strip()
            i += 1
            continue

        # Regular paragraph - collect consecutive lines
        flush_list()
        para = stripped
        while (i + 1 < len(lines)
               and lines[i+1].strip()
               and not lines[i+1].strip().startswith('#')
               and not lines[i+1].strip().startswith('- ')
               and not re.match(r'^\d+\.', lines[i+1].strip())
               and not lines[i+1].strip().startswith('|')
               and not lines[i+1].strip() == '---'
               and not lines[i+1].strip().startswith('!')
               and not lines[i+1].strip().startswith('*FIG')):
            i += 1
            para += ' ' + lines[i].strip()

        pdf.body_text(para)
        i += 1

    flush_list()

    pdf.output(PDF_FILE)
    print(f"Specification PDF generated: {PDF_FILE}")
    print(f"Pages: {pdf.page_no()}")
    print(f"Size: {os.path.getsize(PDF_FILE) / 1024:.0f} KB")


def generate_figures_pdf():
    """Generate a separate PDF containing only the patent figures."""
    FIGURES_PDF = os.path.join(PATENT_DIR, "utility_patent_figures.pdf")

    pdf = PatentPDF()
    pdf.alias_nb_pages()

    figures = [
        ("figures/coral-demo-shot-2.png", "FIG. 1 — Web Dashboard Screenshot showing the system in operation with multiple AI agents, terminal output, and inter-agent message board."),
        ("figures/U1_system_architecture.png", "FIG. 2 — System Architecture Diagram showing the major system components."),
        ("figures/U2_agent_spawning.png", "FIG. 3 — Agent Spawning Flowchart showing the agent team provisioning process."),
        ("figures/U3_status_protocol_detection.png", "FIG. 4 — Inline Status Protocol Detection Flowchart showing the real-time inline status extraction cycle."),
        ("figures/U4_message_board_cursor.png", "FIG. 5 — Message Board Cursor Mechanism Flowchart showing the read and post operations with cursor advancement."),
        ("figures/U5_session_sleep_wake.png", "FIG. 6 — Session Sleep/Wake State Diagram showing the agent session lifecycle with sleep/wake transitions."),
        ("figures/U6_subscription_transfer.png", "FIG. 7 — Subscription Transfer Sequence Diagram illustrating subscription transfer during session restart."),
        ("figures/U7_agent_communication_abstraction.png", "FIG. 8 — Agent Communication Abstraction Layer illustrating the self-contained behavior prompt and communication abstraction."),
    ]

    for img_rel, caption in figures:
        img_path = os.path.join(PATENT_DIR, img_rel)
        if not os.path.exists(img_path):
            print(f"WARNING: {img_path} not found, skipping")
            continue

        pdf.add_page()
        pdf.set_font("TNR", "B", 14)
        fig_label = caption.split("—")[0].strip()
        pdf.cell(0, 10, fig_label, align="C")
        pdf.ln(12)

        # Scale image to fit within available page area
        available_w = pdf.w - pdf.l_margin - pdf.r_margin
        available_h = pdf.h - pdf.get_y() - 30  # leave room for caption + margin
        try:
            # Get image dimensions to compute aspect ratio
            from PIL import Image as PILImage
            with PILImage.open(img_path) as img:
                img_w, img_h = img.size
            aspect = img_h / img_w

            # Scale to fit: try width first, check if height overflows
            render_w = available_w
            render_h = render_w * aspect
            if render_h > available_h:
                # Height-constrained: scale to fit height instead
                render_h = available_h
                render_w = render_h / aspect

            # Center horizontally
            x_offset = pdf.l_margin + (available_w - render_w) / 2
            pdf.image(img_path, x=x_offset, w=render_w, h=render_h)
        except Exception as e:
            pdf.set_font("TNR", "", 12)
            pdf.cell(0, 10, f"[Image could not be embedded: {e}]")

        pdf.ln(4)
        pdf.set_font("TNR", "I", 10)
        pdf.multi_cell(0, 5, caption, align="C")

    pdf.output(FIGURES_PDF)
    print(f"Figures PDF generated: {FIGURES_PDF}")
    print(f"Pages: {pdf.page_no()}")
    print(f"Size: {os.path.getsize(FIGURES_PDF) / 1024:.0f} KB")


if __name__ == "__main__":
    generate_pdf()
    generate_figures_pdf()
