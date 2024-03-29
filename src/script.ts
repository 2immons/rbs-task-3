// script.js
import './style.css';
import Controller from './controller';
import View from './view';

const controller = new Controller();
const view = new View(controller);

document.addEventListener("DOMContentLoaded", function() {
    controller.init();
});

