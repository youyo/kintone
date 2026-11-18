// vite の `?raw` import(README などの文書を文字列で読む)の型。
declare module "*.md?raw" {
  const content: string;
  export default content;
}

// src/feedback/ が env などを import しないことを、ソースの import 文から確かめるテスト用(ADR 0013)。
declare module "*.ts?raw" {
  const content: string;
  export default content;
}
