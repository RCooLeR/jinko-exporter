export const setClassNameIfChanged = (node: Element, className: string): void => {
  if (node.className !== className) {
    node.className = className;
  }
};

export const setTextContentIfChanged = (node: Node, text: string): void => {
  if (node.textContent !== text) {
    node.textContent = text;
  }
};

export const setHiddenIfChanged = (node: HTMLElement, hidden: boolean): void => {
  if (node.hidden !== hidden) {
    node.hidden = hidden;
  }
};

export const setStyleIfChanged = (node: HTMLElement, property: string, value: string): void => {
  if (node.style.getPropertyValue(property) !== value) {
    node.style.setProperty(property, value);
  }
};
